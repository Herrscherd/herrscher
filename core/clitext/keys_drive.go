package clitext

// Keys of the drive verbs, of `herrscher drive add` and of the drives line of health.
const (
	CmdHelpDriveList      = "cli.help.cmd.drive.list"
	CmdHelpDriveLs        = "cli.help.cmd.drive.ls"
	CmdHelpDriveRead      = "cli.help.cmd.drive.read"
	CmdHelpDriveWrite     = "cli.help.cmd.drive.write"
	CmdHelpDriveLog       = "cli.help.cmd.drive.log"
	CmdHelpDriveGrant     = "cli.help.cmd.drive.grant"
	CmdHelpDriveRemove    = "cli.help.cmd.drive.remove"
	CmdParamDriveName     = "cli.help.param.drive.drive"
	CmdParamDrivePath     = "cli.help.param.drive.path"
	CmdParamDriveTo       = "cli.help.param.drive.to"
	CmdParamDriveFrom     = "cli.help.param.drive.from"
	CmdParamDriveAgent    = "cli.help.param.drive.agent"
	CmdParamDriveAccess   = "cli.help.param.drive.access"
	CmdParamDriveSession  = "cli.help.param.drive.session"
	CmdParamDriveLimit    = "cli.help.param.drive.limit"
	HostDriveNone         = "cli.host.drive.none"
	HostDriveEmptyFolder  = "cli.host.drive.empty_folder"
	HostDriveTruncated    = "cli.host.drive.truncated"
	HostDriveCopied       = "cli.host.drive.copied"
	HostDriveWrote        = "cli.host.drive.wrote"
	HostDriveGranted      = "cli.host.drive.granted"
	HostDriveRemoved      = "cli.host.drive.removed"
	HostDriveLogEmpty     = "cli.host.drive.log_empty"
	DriveUsage            = "cli.drive.usage"
	DriveAdded            = "cli.drive.added"
	DriveSecretPrompt     = "cli.drive.secret_prompt"
	DriveSecretMissing    = "cli.drive.secret_missing"
	DriveSignInStarting   = "cli.drive.sign_in_starting"
	DriveAccountRequired  = "cli.drive.account_required"
	DriveAccessKeyMissing = "cli.drive.access_key_missing"
	HealthDrivesFacts     = "cli.health.facts.drives"
	HealthDrivesNoRclone  = "cli.health.facts.drives_no_rclone"
)

// ParamDrive is the parameter that carries a drive name.
const ParamDrive = "Drive"

func driveKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpDriveList:      nil,
		CmdHelpDriveLs:        nil,
		CmdHelpDriveRead:      nil,
		CmdHelpDriveWrite:     nil,
		CmdHelpDriveLog:       nil,
		CmdHelpDriveGrant:     nil,
		CmdHelpDriveRemove:    nil,
		CmdParamDriveName:     nil,
		CmdParamDrivePath:     nil,
		CmdParamDriveTo:       nil,
		CmdParamDriveFrom:     nil,
		CmdParamDriveAgent:    nil,
		CmdParamDriveAccess:   nil,
		CmdParamDriveSession:  nil,
		CmdParamDriveLimit:    nil,
		HostDriveNone:         nil,
		HostDriveEmptyFolder:  {ParamDrive, ParamPath},
		HostDriveTruncated:    {ParamLimit},
		HostDriveCopied:       {ParamBytes, ParamDrive, ParamPath, ParamTo},
		HostDriveWrote:        {ParamBytes, ParamDrive, ParamPath},
		HostDriveGranted:      {ParamDrive, ParamAgent, ParamAccess},
		HostDriveRemoved:      {ParamDrive},
		HostDriveLogEmpty:     nil,
		DriveUsage:            {ParamKind},
		DriveAdded:            {ParamDrive, ParamKind, ParamAccess},
		DriveSecretPrompt:     nil,
		DriveSecretMissing:    {ParamName},
		DriveSignInStarting:   {ParamKind},
		DriveAccountRequired:  nil,
		DriveAccessKeyMissing: nil,
		HealthDrivesFacts:     {ParamVersion, ParamCount},
		HealthDrivesNoRclone:  {ParamCommand, ParamCount},
	}
}
