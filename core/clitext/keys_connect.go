package clitext

// Keys of the connect command: the lines of connect github, connect status and connect logout.
const (
	ConnectUsage          = "cli.connect.usage"
	ConnectHostInvalid    = "cli.connect.host_invalid"
	ConnectStart          = "cli.connect.start"
	ConnectDone           = "cli.connect.done"
	ConnectAppUnavailable = "cli.connect.app_unavailable"
	ConnectDeviceExpired  = "cli.connect.device_expired"
	ConnectDeviceDenied   = "cli.connect.device_denied"
	ConnectFailed         = "cli.connect.failed"
	ConnectStatusLine     = "cli.connect.status.line"
	ConnectSourceNone     = "cli.connect.status.source_none"
	ConnectLoginUnknown   = "cli.connect.status.login_unknown"
	ConnectAccessNever    = "cli.connect.status.access_never"
	ConnectAccessExpires  = "cli.connect.status.access_expires"
	ConnectAccessRejected = "cli.connect.status.access_rejected"
	ConnectInstallLine    = "cli.connect.status.install"
	ConnectRevokeLine     = "cli.connect.status.revoke"
	ConnectLogoutDone     = "cli.connect.logout_done"
	ConnectLogoutNothing  = "cli.connect.logout_nothing"
	ConnectLogoutRevoke   = "cli.connect.logout_revoke"
	ConnectStoreCorrupt   = "cli.connect.store_corrupt"
	ConnectInsecure       = "cli.connect.status.insecure"
	ConnectAccessExpired  = "cli.connect.status.access_expired"
	ConnectAccessRenews   = "cli.connect.status.access_renews"
	ConnectRenewSoon      = "cli.connect.status.renew_soon"
	ConnectAccessAppBad   = "cli.connect.status.access_app_misconfigured"
	ParamUserCode         = "UserCode"
	ParamLogin            = "Login"
	ParamAccess           = "Access"
)

func connectKeys() map[Key][]string {
	return map[Key][]string{
		ConnectUsage:          nil,
		ConnectHostInvalid:    {ParamErr},
		ConnectStart:          {ParamHost, ParamURL, ParamUserCode},
		ConnectDone:           {ParamHost},
		ConnectAppUnavailable: {ParamHost},
		ConnectDeviceExpired:  nil,
		ConnectDeviceDenied:   nil,
		ConnectFailed:         {ParamHost, ParamErr},
		ConnectStatusLine:     {ParamHost, ParamSource, ParamLogin, ParamAccess},
		ConnectSourceNone:     nil,
		ConnectLoginUnknown:   nil,
		ConnectAccessNever:    nil,
		ConnectAccessExpires:  {ParamTime},
		ConnectAccessRejected: nil,
		ConnectInstallLine:    {ParamURL},
		ConnectRevokeLine:     {ParamURL},
		ConnectLogoutDone:     {ParamHost},
		ConnectLogoutNothing:  {ParamHost},
		ConnectLogoutRevoke:   {ParamURL},
		ConnectStoreCorrupt:   {ParamPath},
		ConnectInsecure:       {ParamPath},
		ConnectAccessExpired:  nil,
		ConnectAccessRenews:   {ParamTime},
		ConnectRenewSoon:      {ParamTime},
		ConnectAccessAppBad:   nil,
	}
}
