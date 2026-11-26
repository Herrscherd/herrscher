package clitext

// Names of the params the device replies share.
const (
	ParamCode        = "Code"
	ParamFingerprint = "Fingerprint"
)

// Keys of the replies and help texts of the device commands.
const (
	HostDevicePaired          = "cli.host.device.paired"
	HostDeviceApproved        = "cli.host.device.approved"
	HostDeviceDenied          = "cli.host.device.denied"
	HostDeviceRevoked         = "cli.host.device.revoked"
	HostDeviceNone            = "cli.host.device.none"
	HostDeviceRow             = "cli.host.device.row"
	HostDevicePending         = "cli.host.device.pending"
	HostDeviceStatusActive    = "cli.host.device.status_active"
	HostDeviceStatusExpired   = "cli.host.device.status_expired"
	HostDeviceStatusRevoked   = "cli.host.device.status_revoked"
	HostDeviceIDMissing       = "cli.host.fault.device_id_missing"
	HostDeviceCodeMissing     = "cli.host.fault.device_code_missing"
	DeviceFaultMalformedID    = "cli.host.fault.device_malformed_id"
	DeviceFaultMalformedCode  = "cli.host.fault.device_malformed_code"
	DeviceFaultRole           = "cli.host.fault.device_role"
	DeviceFaultWindowOpen     = "cli.host.fault.device_window_open"
	DeviceFaultNoWindow       = "cli.host.fault.device_no_window"
	DeviceFaultExpired        = "cli.host.fault.device_expired"
	DeviceFaultRedeemed       = "cli.host.fault.device_redeemed"
	DeviceFaultNothingPending = "cli.host.fault.device_nothing_pending"
	DeviceFaultWrongCode      = "cli.host.fault.device_wrong_code"
	DeviceFaultTooMany        = "cli.host.fault.device_too_many"
	DeviceFaultBadRequest     = "cli.host.fault.device_bad_request"
	DeviceFaultUnsupportedKey = "cli.host.fault.device_unsupported_key"
	DeviceFaultUnknown        = "cli.host.fault.device_unknown"
	DeviceFaultAuthority      = "cli.host.fault.device_authority"
	CmdHelpDevicePair         = "cli.help.cmd.device.pair"
	CmdHelpDeviceApprove      = "cli.help.cmd.device.approve"
	CmdHelpDeviceDeny         = "cli.help.cmd.device.deny"
	CmdHelpDeviceList         = "cli.help.cmd.device.list"
	CmdHelpDeviceRevoke       = "cli.help.cmd.device.revoke"
	CmdParamDevicePairName    = "cli.help.param.device.pair.name"
	CmdParamDevicePairRole    = "cli.help.param.device.pair.role"
	CmdParamDeviceApproveCode = "cli.help.param.device.approve.code"
	CmdParamDeviceDenyCode    = "cli.help.param.device.deny.code"
	CmdParamDeviceRevokeID    = "cli.help.param.device.revoke.id"
)

func deviceKeys() map[Key][]string {
	return map[Key][]string{
		HostDevicePaired:          {ParamCode, ParamRole},
		HostDeviceApproved:        {ParamID, ParamName, ParamRole, ParamFingerprint},
		HostDeviceDenied:          nil,
		HostDeviceRevoked:         {ParamID},
		HostDeviceNone:            nil,
		HostDeviceRow:             {ParamID, ParamName, ParamRole, ParamStatus, ParamWhen},
		HostDevicePending:         {ParamName, ParamRole, ParamFingerprint},
		HostDeviceStatusActive:    nil,
		HostDeviceStatusExpired:   nil,
		HostDeviceStatusRevoked:   nil,
		HostDeviceIDMissing:       nil,
		HostDeviceCodeMissing:     nil,
		DeviceFaultMalformedID:    nil,
		DeviceFaultMalformedCode:  nil,
		DeviceFaultRole:           nil,
		DeviceFaultWindowOpen:     nil,
		DeviceFaultNoWindow:       nil,
		DeviceFaultExpired:        nil,
		DeviceFaultRedeemed:       nil,
		DeviceFaultNothingPending: nil,
		DeviceFaultWrongCode:      nil,
		DeviceFaultTooMany:        nil,
		DeviceFaultBadRequest:     nil,
		DeviceFaultUnsupportedKey: nil,
		DeviceFaultUnknown:        nil,
		DeviceFaultAuthority:      nil,
		CmdHelpDevicePair:         nil,
		CmdHelpDeviceApprove:      nil,
		CmdHelpDeviceDeny:         nil,
		CmdHelpDeviceList:         nil,
		CmdHelpDeviceRevoke:       nil,
		CmdParamDevicePairName:    nil,
		CmdParamDevicePairRole:    nil,
		CmdParamDeviceApproveCode: nil,
		CmdParamDeviceDenyCode:    nil,
		CmdParamDeviceRevokeID:    nil,
	}
}
