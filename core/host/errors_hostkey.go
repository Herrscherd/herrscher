package host

import "errors"

type hostDetailError struct {
	cause  error
	detail string
}

func (e *hostDetailError) Error() string {
	return e.cause.Error() + ": " + e.detail
}

func (e *hostDetailError) Unwrap() error {
	return e.cause
}

func withDetail(cause error, detail string) error {
	return &hostDetailError{cause: cause, detail: detail}
}

var (
	errHostKeyMismatch    = errors.New("the host key given does not match the one shown: nothing was registered")
	errHostKeyChanged     = errors.New("the host key changed since it was confirmed: the host takes no session until its new key is trusted")
	errHostKeyPinMissing  = errors.New("the host has a pinned key but its known_hosts file is missing: run `host trust` again")
	errHostFull           = errors.New("the host is at its session limit")
	errHostWaitLimit      = errors.New("the host stayed full past the wait limit")
	errProjectNotAllowed  = errors.New("the project is not on the allowed list of the host")
	errMaxSessionsInvalid = errors.New("max-sessions must be a whole number, zero for no cap")
	errHostUnreachable    = errors.New("the host cannot be reached over ssh")
	errSetConflict        = errors.New("--allow-project and --allow-all-projects cannot be given together")
	errSetNothing         = errors.New("host set needs at least one change: --allow-project, --allow-all-projects, --max-sessions or --when-full")
)
