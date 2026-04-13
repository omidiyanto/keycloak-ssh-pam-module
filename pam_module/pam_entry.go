//go:build linux

package main

/*
#cgo LDFLAGS: -lpam -fPIC
#include <security/pam_appl.h>
#include <stdlib.h>

// IMPORTANT: Do NOT include <security/pam_modules.h> here to avoid
// conflicting types of pam_sm_* functions when CGo exports them.
// The constants like PAM_SUCCESS are implicitly dragged in from pam_appl.h anyway.

// Forward declarations of C helpers defined in pam_conv.go
char* get_pam_user(pam_handle_t *pamh);
int   get_user_uid(const char *user);
*/
import "C"

import (
	"fmt"
	"log/syslog"
	"os"
	"runtime"
	"strings"
	"unsafe"
)

// ============================================================================
// Syslog Logger
// ============================================================================

var pamLogger *syslog.Writer

func getLogger() *syslog.Writer {
	if pamLogger == nil {
		l, err := syslog.New(syslog.LOG_AUTH|syslog.LOG_INFO, "pam_keycloak_device")
		if err == nil {
			pamLogger = l
		}
	}
	return pamLogger
}

func pamLog(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if logger := getLogger(); logger != nil {
		logger.Info(msg)
	}
}

func pamLogError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if logger := getLogger(); logger != nil {
		logger.Err(msg)
	}
}

// ============================================================================
// PAM Entry Points (exported to C)
// ============================================================================

// sliceFromArgv converts C argc/argv to a Go string slice.
func sliceFromArgv(argc C.int, argv **C.char) []string {
	if argc == 0 || argv == nil {
		return nil
	}
	r := make([]string, 0, int(argc))
	for i := 0; i < int(argc); i++ {
		// Calculate pointer to argv[i]
		ptr := (**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(argv)) + uintptr(i)*unsafe.Sizeof(*argv)))
		if *ptr != nil {
			r = append(r, C.GoString(*ptr))
		}
	}
	return r
}

// parseConfigPath extracts the config= argument from PAM module arguments.
func parseConfigPath(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "config=") {
			return strings.TrimPrefix(arg, "config=")
		}
	}
	return "" // use default
}

//export pam_sm_authenticate
func pam_sm_authenticate(pamh *C.pam_handle_t, flags, argc C.int, argv **C.char) C.int {
	debugLog("pam_sm_authenticate CALLED!")


	username := pamGetUser(pamh)
	if username == "" {
		debugLog("failed to get username from PAM handle")
		pamLogError("failed to get username from PAM handle")
		return C.PAM_USER_UNKNOWN
	}

	uid := pamGetUserUID(username)
	if uid < 0 {
		debugLog("failed to get UID for user: %s", username)
		pamLogError("failed to get UID for user: %s", username)
		return C.PAM_USER_UNKNOWN
	}

	// Parse module arguments for config path
	args := sliceFromArgv(argc, argv)
	configPath := parseConfigPath(args)

	debugLog("starting Keycloak device flow authentication for user: %s (uid: %d)", username, uid)
	pamLog("starting Keycloak device flow authentication for user: %s (uid: %d)", username, uid)

	// Create PAM conversation callbacks for auth.go (pure Go code)
	conv := &PAMConv{
		SendInfo: func(msg string) {
			pamSendInfo(pamh, msg)
		},
		SendError: func(msg string) {
			pamSendError(pamh, msg)
		},
		Prompt: func(prompt string) string {
			return pamPrompt(pamh, prompt, true)
		},
	}

	result := performDeviceFlowAuth(username, configPath, conv)
	if result != AuthSuccess {
		pamLogError("authentication failed for user: %s", username)
		return C.PAM_AUTH_ERR
	}

	pamLog("authentication succeeded for user: %s", username)
	return C.PAM_SUCCESS
}

//export pam_sm_setcred
func pam_sm_setcred(pamh *C.pam_handle_t, flags, argc C.int, argv **C.char) C.int {
	return C.PAM_IGNORE
}

//export pam_sm_acct_mgmt
func pam_sm_acct_mgmt(pamh *C.pam_handle_t, flags, argc C.int, argv **C.char) C.int {
	return C.PAM_IGNORE
}

//export pam_sm_open_session
func pam_sm_open_session(pamh *C.pam_handle_t, flags, argc C.int, argv **C.char) C.int {
	// Session tracking is handled during pam_sm_authenticate
	// because we need the Keycloak token response data (session_state)
	// which is only available at authentication time.
	return C.PAM_SUCCESS
}

//export pam_sm_close_session
func pam_sm_close_session(pamh *C.pam_handle_t, flags, argc C.int, argv **C.char) C.int {


	username := pamGetUser(pamh)
	if username == "" {
		return C.PAM_SUCCESS
	}

	args := sliceFromArgv(argc, argv)
	configPath := parseConfigPath(args)

	cleanupSession(username, configPath)
	return C.PAM_SUCCESS
}

// getSSHPid returns the PID of the current sshd child process.
// In the PAM module context (loaded as .so into sshd), os.Getpid()
// returns the sshd child process PID handling this SSH connection.
// Killing this PID terminates the entire SSH session.
func getSSHPid() int {
	return os.Getpid()
}
