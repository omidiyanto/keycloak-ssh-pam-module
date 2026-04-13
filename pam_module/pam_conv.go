//go:build linux

package main

/*
#include <security/pam_appl.h>
#include <security/pam_modules.h>
#include <stdlib.h>
#include <string.h>
#include <pwd.h>

// ============================================================================
// C Helper Functions for PAM Conversation
// ============================================================================

// get_pam_user extracts the username from the PAM handle.
static char* get_pam_user(pam_handle_t *pamh) {
    if (!pamh) return NULL;
    const char *user = NULL;
    if (pam_get_item(pamh, PAM_USER, (const void**)&user) != PAM_SUCCESS)
        return NULL;
    return user ? strdup(user) : NULL;
}

// get_user_uid returns the UID for the given username.
static int get_user_uid(const char *user) {
    if (!user) return -1;
    struct passwd *pw = getpwnam(user);
    if (!pw) return -1;
    return (int)pw->pw_uid;
}

// send_pam_msg sends a message to the user via the PAM conversation function.
// msg_style can be PAM_TEXT_INFO, PAM_ERROR_MSG, PAM_PROMPT_ECHO_ON, or PAM_PROMPT_ECHO_OFF.
static int send_pam_msg(pam_handle_t *pamh, int msg_style, const char *msg, char **response) {
    if (!pamh || !msg) return PAM_CONV_ERR;

    const struct pam_conv *conv = NULL;
    if (pam_get_item(pamh, PAM_CONV, (const void**)&conv) != PAM_SUCCESS || !conv)
        return PAM_CONV_ERR;

    struct pam_message pmsg;
    pmsg.msg_style = msg_style;
    pmsg.msg = msg;

    const struct pam_message *pmsgp = &pmsg;
    struct pam_response *resp = NULL;

    int ret = conv->conv(1, &pmsgp, &resp, conv->appdata_ptr);

    if (resp) {
        // If caller wants the response text, pass it back
        if (response && resp->resp) {
            *response = resp->resp;  // caller is responsible for freeing
        } else if (resp->resp) {
            free(resp->resp);
        }
        free(resp);
    }

    return ret;
}

// send_pam_info sends a PAM_TEXT_INFO message (displayed to user, no input expected).
static int send_pam_info(pam_handle_t *pamh, const char *msg) {
    return send_pam_msg(pamh, PAM_TEXT_INFO, msg, NULL);
}

// send_pam_error sends a PAM_ERROR_MSG message (displayed as error to user).
static int send_pam_error(pam_handle_t *pamh, const char *msg) {
    return send_pam_msg(pamh, PAM_ERROR_MSG, msg, NULL);
}

// send_pam_prompt sends a prompt and returns the user's response.
// echo_on: 1 for PAM_PROMPT_ECHO_ON, 0 for PAM_PROMPT_ECHO_OFF.
// Returns the user's response (caller must free) or NULL on error.
static char* send_pam_prompt(pam_handle_t *pamh, const char *prompt, int echo_on) {
    if (!pamh || !prompt) return NULL;

    int style = echo_on ? PAM_PROMPT_ECHO_ON : PAM_PROMPT_ECHO_OFF;
    char *response = NULL;

    if (send_pam_msg(pamh, style, prompt, &response) != PAM_SUCCESS) {
        if (response) free(response);
        return NULL;
    }

    return response;
}
*/
import "C"

import "unsafe"

// pamSendInfo sends an informational message to the SSH client terminal.
// This maps to PAM_TEXT_INFO which SSH displays as keyboard-interactive info text.
func pamSendInfo(pamh *C.pam_handle_t, msg string) {
	cmsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cmsg))
	C.send_pam_info(pamh, cmsg)
}

// pamSendError sends an error message to the SSH client terminal.
// This maps to PAM_ERROR_MSG which SSH displays as an error.
func pamSendError(pamh *C.pam_handle_t, msg string) {
	cmsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cmsg))
	C.send_pam_error(pamh, cmsg)
}

// pamPrompt sends a prompt to the SSH client and blocks until the user responds.
// When echoOn is true, the user's input is visible (PAM_PROMPT_ECHO_ON).
// When echoOn is false, input is hidden (PAM_PROMPT_ECHO_OFF, like password entry).
func pamPrompt(pamh *C.pam_handle_t, prompt string, echoOn bool) string {
	cprompt := C.CString(prompt)
	defer C.free(unsafe.Pointer(cprompt))

	echo := C.int(0)
	if echoOn {
		echo = C.int(1)
	}

	cResp := C.send_pam_prompt(pamh, cprompt, echo)
	if cResp == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(cResp))
	return C.GoString(cResp)
}

// pamGetUser returns the username from the PAM handle.
func pamGetUser(pamh *C.pam_handle_t) string {
	cuser := C.get_pam_user(pamh)
	if cuser == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(cuser))
	return C.GoString(cuser)
}

// pamGetUserUID returns the numeric UID for the given username.
func pamGetUserUID(username string) int {
	cuser := C.CString(username)
	defer C.free(unsafe.Pointer(cuser))
	return int(C.get_user_uid(cuser))
}
