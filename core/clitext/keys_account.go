package clitext

// Keys of the account command: its usage, its listing, and the prompts and
// refusals of add, remove and default.
const (
	AccountUsage             = "cli.account.usage"
	AccountResolveHome       = "cli.account.resolve_home"
	AccountDefault           = "cli.account.default"
	AccountDefaultMarker     = "cli.account.default_marker"
	AccountNativeLabel       = "cli.account.native_label"
	AccountStateUnknown      = "cli.account.state.unknown"
	AccountStateConnected    = "cli.account.state.connected"
	AccountStateDisconnected = "cli.account.state.disconnected"
	AccountLoginFailed       = "cli.account.login_failed"
	AccountAdded             = "cli.account.added"
	AccountRemovePrompt      = "cli.account.remove_prompt"
	AccountReadConfirmation  = "cli.account.read_confirmation"
	AccountCancelled         = "cli.account.cancelled"
	AccountInUse             = "cli.account.in_use"
	AccountResuming          = "cli.account.resuming"
)

func accountKeys() map[Key][]string {
	return map[Key][]string{
		AccountUsage:             nil,
		AccountResolveHome:       {ParamErr},
		AccountDefault:           {ParamVendor, ParamName},
		AccountDefaultMarker:     nil,
		AccountNativeLabel:       nil,
		AccountStateUnknown:      nil,
		AccountStateConnected:    nil,
		AccountStateDisconnected: nil,
		AccountLoginFailed:       {ParamVendor, ParamName, ParamErr},
		AccountAdded:             {ParamVendor, ParamName, ParamState},
		AccountRemovePrompt:      {ParamVendor, ParamName},
		AccountReadConfirmation:  {ParamErr},
		AccountCancelled:         nil,
		AccountInUse:             {ParamVendor, ParamName, ParamSessions},
		AccountResuming:          {ParamCount, ParamVendor, ParamSessions},
	}
}
