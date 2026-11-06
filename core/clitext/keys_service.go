package clitext

// Keys of the service command and of the service package it drives.
const (
	ServiceUsage                  = "cli.service.usage"
	ServiceUnknownSubcommand      = "cli.service.unknown_subcommand"
	ServiceUnexpectedArgument     = "cli.service.unexpected_argument"
	ServiceUpdateUnexpectedArg    = "cli.service.update.unexpected_argument"
	ServiceRestarted              = "cli.service.restarted"
	ServiceRemoved                = "cli.service.removed"
	ServiceUpdateRebuilding       = "cli.service.update.rebuilding"
	ServiceUpdateRebuilt          = "cli.service.update.rebuilt"
	ServiceFlagHealthAddr         = "cli.service.flag.health_addr"
	ServiceFlagEnvFile            = "cli.service.flag.env_file"
	ServiceFlagCmd                = "cli.service.flag.cmd"
	ServiceFlagSource             = "cli.service.flag.source"
	ServiceFlagNoPull             = "cli.service.flag.no_pull"
	ServiceUnsupportedOS          = "cli.service.unsupported_os"
	ServiceNoSourceDir            = "cli.service.no_source_dir"
	ServiceNotCheckoutNoModule    = "cli.service.not_checkout.no_module"
	ServiceNotCheckoutWrongModule = "cli.service.not_checkout.wrong_module"
	ServiceNoGoToolchain          = "cli.service.no_go_toolchain"
	ServiceNoBinaryPath           = "cli.service.no_binary_path"
	ServiceGitPullFailed          = "cli.service.git_pull_failed"
	ServiceGoBuildFailed          = "cli.service.go_build_failed"
	ServiceGoBuildForFailed       = "cli.service.go_build_for_failed"
	ServiceSmokeFailed            = "cli.service.smoke_failed"
	ServiceLocateBinary           = "cli.service.locate_binary"
	ServiceLocateHome             = "cli.service.locate_home"
	ServiceStatFailed             = "cli.service.stat_failed"
	ServiceMkdirFailed            = "cli.service.mkdir_failed"
	ServiceWriteFailed            = "cli.service.write_failed"
	ServiceNoteUnitPath           = "cli.service.note.unit_path"
	ServiceNoteNotStarted         = "cli.service.note.not_started"
	ServiceNoteRunning            = "cli.service.note.running"
	ServiceNoteWindowsEdit        = "cli.service.note.windows_edit"
	ServiceEnvOwnerID             = "cli.service.env.owner_id"
)

func serviceKeys() map[Key][]string {
	return map[Key][]string{
		ServiceUsage:                  nil,
		ServiceEnvOwnerID:             nil,
		ServiceUnknownSubcommand:      {ParamName},
		ServiceUnexpectedArgument:     {ParamVerb, ParamArg, ParamUsage},
		ServiceUpdateUnexpectedArg:    {ParamArg},
		ServiceRestarted:              nil,
		ServiceRemoved:                nil,
		ServiceUpdateRebuilding:       {ParamInstalled, ParamRan},
		ServiceUpdateRebuilt:          {ParamVersion},
		ServiceFlagHealthAddr:         nil,
		ServiceFlagEnvFile:            nil,
		ServiceFlagCmd:                nil,
		ServiceFlagSource:             nil,
		ServiceFlagNoPull:             nil,
		ServiceUnsupportedOS:          {ParamOS},
		ServiceNoSourceDir:            nil,
		ServiceNotCheckoutNoModule:    {ParamPath, ParamErr},
		ServiceNotCheckoutWrongModule: {ParamPath},
		ServiceNoGoToolchain:          nil,
		ServiceNoBinaryPath:           nil,
		ServiceGitPullFailed:          {ParamErr, ParamOutput},
		ServiceGoBuildFailed:          {ParamErr, ParamOutput},
		ServiceGoBuildForFailed:       {ParamOS, ParamArch, ParamErr, ParamOutput},
		ServiceSmokeFailed:            {ParamPath, ParamErr, ParamOutput},
		ServiceLocateBinary:           {ParamErr},
		ServiceLocateHome:             {ParamErr},
		ServiceStatFailed:             {ParamPath, ParamErr},
		ServiceMkdirFailed:            {ParamPath, ParamErr},
		ServiceWriteFailed:            {ParamPath, ParamErr},
		ServiceNoteUnitPath:           nil,
		ServiceNoteNotStarted:         {ParamEnvFile, ParamStart},
		ServiceNoteRunning:            {ParamEnvFile},
		ServiceNoteWindowsEdit:        {ParamEnvFile, ParamTask},
	}
}
