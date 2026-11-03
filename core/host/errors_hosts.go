package host

import (
	"fmt"
)

type gatewayEnvUnsupportedError struct {
	Model string
}

func (e *gatewayEnvUnsupportedError) Error() string {
	return fmt.Sprintf("model %q needs a gateway spawn environment, which the remote backend resolver cannot carry; unset HERRSCHER_REMOTE=backend or pick a native model", e.Model)
}

type hostNeedsSSHError struct {
	Name string
}

func (e *hostNeedsSSHError) Error() string {
	return fmt.Sprintf("host %q needs an ssh target, e.g. --ssh me@%s", e.Name, e.Name)
}

type hostWorkspaceError struct {
	Name      string
	Workspace string
}

func (e *hostWorkspaceError) Error() string {
	return fmt.Sprintf("host %q needs an absolute workspace path, got %q", e.Name, e.Workspace)
}

type hostExistsError struct {
	Name string
}

func (e *hostExistsError) Error() string {
	return fmt.Sprintf("a host named %q already exists (remove it first, or run `host provision %s`)", e.Name, e.Name)
}

type hostNotProvisionedError struct {
	Name string
}

func (e *hostNotProvisionedError) Error() string {
	return fmt.Sprintf("host %q is not provisioned: run `host provision %s`", e.Name, e.Name)
}

type hostNameRequiredError struct {
	Known string
}

func (e *hostNameRequiredError) Error() string {
	return fmt.Sprintf("name a host (known: %s)", e.Known)
}

type hostNotFoundError struct {
	Name  string
	Known string
}

func (e *hostNotFoundError) Error() string {
	if e.Known == "" {
		return fmt.Sprintf("no host named %q, and none is registered: add one with `host add <name> --ssh <target> --workspace <path>`", e.Name)
	}
	return fmt.Sprintf("no host named %q (known: %s)", e.Name, e.Known)
}

type hostCarriesError struct {
	Name     string
	Sessions string
}

func (e *hostCarriesError) Error() string {
	return fmt.Sprintf("host %q still carries %s: close them first", e.Name, e.Sessions)
}

type hostVersionMismatchError struct {
	Name string
	Have string
	Want string
}

func (e *hostVersionMismatchError) Error() string {
	return fmt.Sprintf("host %q runs herrscher %s but this daemon builds %s: run `host provision %s`", e.Name, e.Have, e.Want, e.Name)
}

type originReadError struct {
	Dir string
	Msg string
}

func (e *originReadError) Error() string {
	return fmt.Sprintf("read origin of %s: %s", e.Dir, e.Msg)
}

type noOriginError struct {
	Dir string
}

func (e *noOriginError) Error() string {
	return e.Dir + " has no origin remote"
}

type lockHeldError struct {
	StatePath string
	Holder    string
}

func (e *lockHeldError) Error() string {
	return fmt.Sprintf(
		"another herrscher is already serving %s%s\n"+
			"Only one daemon may serve a state file: a second one answers every message a second time.\n"+
			"On a terminal, a bare `herrscher` attaches to that daemon instead of refusing; you are seeing this\n"+
			"because there is no terminal to attach, or because attaching failed (the reason is printed above).\n"+
			"Stop the other one, or pass --state to serve a separate world.",
		e.StatePath, e.Holder)
}

type remoteStepError struct {
	Step  string
	Where string
	Msg   string
}

func (e *remoteStepError) Error() string {
	return fmt.Sprintf("%s on %s: %s", e.Step, e.Where, e.Msg)
}

type copyError struct {
	Where string
	Msg   string
}

func (e *copyError) Error() string {
	return fmt.Sprintf("copy herrscher to %s: %s", e.Where, e.Msg)
}

type platformUnreadableError struct {
	Output string
}

func (e *platformUnreadableError) Error() string {
	return fmt.Sprintf("cannot read the host platform from %q", e.Output)
}

type unsupportedOSError struct {
	OS string
}

func (e *unsupportedOSError) Error() string {
	return fmt.Sprintf("unsupported remote OS %q (linux and darwin only)", e.OS)
}

type unsupportedArchError struct {
	Arch string
}

func (e *unsupportedArchError) Error() string {
	return fmt.Sprintf("unsupported remote architecture %q (x86_64 and aarch64 only)", e.Arch)
}

type noSourceCheckoutError struct {
	Host string
}

func (e *noSourceCheckoutError) Error() string {
	return fmt.Sprintf("no source checkout configured, so there is no binary to build for %s: run `set source <path>` first", e.Host)
}

type remoteHomeUnresolvedError struct {
	Host string
}

func (e *remoteHomeUnresolvedError) Error() string {
	return e.Host + ": cannot resolve the remote home directory"
}

type unsafeTarMemberError struct {
	Member string
	Root   string
}

func (e *unsafeTarMemberError) Error() string {
	return fmt.Sprintf("refusing tar member %q: it lands outside %s", e.Member, e.Root)
}

type noRemoteBackendError struct {
	Kind string
}

func (e *noRemoteBackendError) Error() string {
	return fmt.Sprintf("resolver: no remote backend announced for kind %q", e.Kind)
}

type instanceIDMismatchError struct {
	Have string
	Want string
}

func (e *instanceIDMismatchError) Error() string {
	return fmt.Sprintf("instanceID mismatch: state has %q but %q was requested; changing it would orphan existing sessions", e.Have, e.Want)
}
