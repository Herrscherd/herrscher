package clitext

// Message keys of host key pinning, session limits and host load replies.
const (
	HostKeyPresented         = "cli.host.key.presented"
	HostKeyTrusted           = "cli.host.key.trusted"
	HostKeyUnpinned          = "cli.host.key.unpinned"
	HostKeyChangedNotice     = "cli.host.key.changed"
	HostKeyStatePinned       = "cli.host.key.state_pinned"
	HostKeyStateNotPinned    = "cli.host.key.state_not_pinned"
	HostKeyStateBlocked      = "cli.host.key.state_blocked"
	HostBinaryUnrecorded     = "cli.host.binary.unrecorded"
	HostSessionsLine         = "cli.host.sessions.line"
	HostSessionsUnlimited    = "cli.host.sessions.unlimited"
	HostLoadLine             = "cli.host.load.line"
	HostLoadUnknown          = "cli.host.load.unknown"
	HostListDetail           = "cli.host.list.detail"
	HostSetDone              = "cli.host.set.done"
	HostWarnUnpinned         = "cli.host.warn.unpinned"
	HostFaultKeyMismatch     = "cli.host.fault.key_mismatch"
	HostFaultKeyChanged      = "cli.host.fault.key_changed"
	HostFaultKeyUnreadable   = "cli.host.fault.key_unreadable"
	HostFaultKeyPinMissing   = "cli.host.fault.key_pin_missing"
	HostFaultBinaryChanged   = "cli.host.fault.binary_changed"
	HostFaultDigestUnread    = "cli.host.fault.digest_unreadable"
	HostFaultHostFull        = "cli.host.fault.host_full"
	HostFaultWaitLimit       = "cli.host.fault.wait_limit"
	HostFaultProjectDenied   = "cli.host.fault.project_not_allowed"
	HostFaultWhenFullInvalid = "cli.host.fault.when_full_invalid"
	HostFaultMaxInvalid      = "cli.host.fault.max_sessions_invalid"
	HostFaultKnownHostsPath  = "cli.host.fault.known_hosts_path"
	HostFaultUnreachable     = "cli.host.fault.unreachable"
	HostFaultSetConflict     = "cli.host.fault.set_conflict"
	HostFaultSetNothing      = "cli.host.fault.set_nothing"
	CmdHelpHostTrust         = "cli.help.cmd.host.trust"
	CmdHelpHostSet           = "cli.help.cmd.host.set"
	CmdParamHostAddHostKey   = "cli.help.param.host.add.host_key"
	CmdParamHostAddProjects  = "cli.help.param.host.add.allow_project"
	CmdParamHostAddMax       = "cli.help.param.host.add.max_sessions"
	CmdParamHostAddWhenFull  = "cli.help.param.host.add.when_full"
	CmdParamHostTrustName    = "cli.help.param.host.trust.name"
	CmdParamHostTrustKey     = "cli.help.param.host.trust.host_key"
	CmdParamHostSetName      = "cli.help.param.host.set.name"
	CmdParamHostSetProjects  = "cli.help.param.host.set.allow_project"
	CmdParamHostSetAllowAll  = "cli.help.param.host.set.allow_all_projects"
	CmdParamHostSetMax       = "cli.help.param.host.set.max_sessions"
	CmdParamHostSetWhenFull  = "cli.help.param.host.set.when_full"
)

// Parameter names of host key pinning and host load replies.
const (
	ParamFingerprint = "Fingerprint"
	ParamPinned      = "Pinned"
	ParamMax         = "Max"
	ParamLoad        = "Load"
)

func hostKeyKeys() map[Key][]string {
	return map[Key][]string{
		HostKeyPresented:         {ParamHost, ParamFingerprint},
		HostKeyTrusted:           {ParamHost, ParamFingerprint},
		HostKeyUnpinned:          {ParamHost, ParamFingerprint},
		HostKeyChangedNotice:     {ParamHost, ParamPinned, ParamFingerprint},
		HostKeyStatePinned:       nil,
		HostKeyStateNotPinned:    nil,
		HostKeyStateBlocked:      nil,
		HostBinaryUnrecorded:     {ParamHost},
		HostSessionsLine:         {ParamSessions, ParamMax},
		HostSessionsUnlimited:    nil,
		HostLoadLine:             {ParamLoad},
		HostLoadUnknown:          nil,
		HostListDetail:           {ParamKey, ParamSessions, ParamLoad},
		HostSetDone:              {ParamHost},
		HostWarnUnpinned:         {ParamHost},
		HostFaultKeyMismatch:     nil,
		HostFaultKeyChanged:      nil,
		HostFaultKeyUnreadable:   nil,
		HostFaultKeyPinMissing:   nil,
		HostFaultBinaryChanged:   nil,
		HostFaultDigestUnread:    nil,
		HostFaultHostFull:        nil,
		HostFaultWaitLimit:       nil,
		HostFaultProjectDenied:   nil,
		HostFaultWhenFullInvalid: nil,
		HostFaultMaxInvalid:      nil,
		HostFaultKnownHostsPath:  nil,
		HostFaultUnreachable:     nil,
		HostFaultSetConflict:     nil,
		HostFaultSetNothing:      nil,
		CmdHelpHostTrust:         nil,
		CmdHelpHostSet:           nil,
		CmdParamHostAddHostKey:   nil,
		CmdParamHostAddProjects:  nil,
		CmdParamHostAddMax:       nil,
		CmdParamHostAddWhenFull:  nil,
		CmdParamHostTrustName:    nil,
		CmdParamHostTrustKey:     nil,
		CmdParamHostSetName:      nil,
		CmdParamHostSetProjects:  nil,
		CmdParamHostSetAllowAll:  nil,
		CmdParamHostSetMax:       nil,
		CmdParamHostSetWhenFull:  nil,
	}
}
