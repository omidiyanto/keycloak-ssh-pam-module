#include <security/pam_appl.h>
#include <security/pam_modules.h>

// Include the CGo generated header to access Go exported functions (e.g., go_pam_sm_authenticate)
#include "_cgo_export.h"

// Standard PAM C wrappers with proper 'const char **' signatures.
// These simply cast the arguments and delegate to the pure Go implementations.

PAM_EXTERN int pam_sm_authenticate(pam_handle_t *pamh, int flags, int argc, const char **argv) {
    return go_pam_sm_authenticate(pamh, flags, argc, (char**)argv);
}

PAM_EXTERN int pam_sm_setcred(pam_handle_t *pamh, int flags, int argc, const char **argv) {
    return go_pam_sm_setcred(pamh, flags, argc, (char**)argv);
}

PAM_EXTERN int pam_sm_acct_mgmt(pam_handle_t *pamh, int flags, int argc, const char **argv) {
    return go_pam_sm_acct_mgmt(pamh, flags, argc, (char**)argv);
}

PAM_EXTERN int pam_sm_open_session(pam_handle_t *pamh, int flags, int argc, const char **argv) {
    return go_pam_sm_open_session(pamh, flags, argc, (char**)argv);
}

PAM_EXTERN int pam_sm_close_session(pam_handle_t *pamh, int flags, int argc, const char **argv) {
    return go_pam_sm_close_session(pamh, flags, argc, (char**)argv);
}
