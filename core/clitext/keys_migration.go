package clitext

// ParamTrust names the command that tells git to trust a folder it calls dubious.
const ParamTrust = "Trust"

// Keys of the state format migration, announced in the daemon log and on the output of serve.
const (
	MigrationStarted               = "cli.migration.started"
	MigrationDone                  = "cli.migration.done"
	MigrationInstanceConfigured    = "cli.migration.instance_configured"
	MigrationInstanceGenerated     = "cli.migration.instance_generated"
	MigrationWorkspaceFromRepo     = "cli.migration.workspace_from_repo"
	MigrationRepoDropped           = "cli.migration.repo_dropped"
	MigrationGatewaysBound         = "cli.migration.gateways_bound"
	MigrationWorktreeRelocated     = "cli.migration.worktree_relocated"
	MigrationBranchRenamed         = "cli.migration.branch_renamed"
	MigrationNewerFormat           = "cli.migration.newer_format"
	MigrationPending               = "cli.migration.pending"
	MigrationNoGateway             = "cli.migration.no_gateway"
	MigrationRemoteWorktree        = "cli.migration.remote_worktree"
	MigrationRemoteScratch         = "cli.migration.remote_scratch"
	MigrationConversationReset     = "cli.migration.conversation_reset"
	MigrationBackupFailed          = "cli.migration.backup_failed"
	MigrationRefused               = "cli.migration.refused"
	MigrationCheckpointsRekeyed    = "cli.migration.checkpoints_rekeyed"
	MigrationUndoRekeyed           = "cli.migration.undo_rekeyed"
	MigrationRekeyFinished         = "cli.migration.rekey_finished"
	MigrationCheckpointsRenumbered = "cli.migration.checkpoints_renumbered"
	MigrationUndoReplaced          = "cli.migration.undo_replaced"
	MigrationCheckpointInspect     = "cli.migration.checkpoint_inspect"
	MigrationCheckpointHidden      = "cli.migration.checkpoint_hidden_repository"
	MigrationCheckpointMove        = "cli.migration.checkpoint_move"
	MigrationSharedWorktree        = "cli.migration.shared_worktree"
	MigrationInterrupted           = "cli.migration.interrupted"
	MigrationNothingDone           = "cli.migration.nothing_done"
	InstanceGenerated              = "cli.instance.generated"
	InstanceStoredInvalid          = "cli.instance.stored_invalid"
)

func migrationKeys() map[Key][]string {
	return map[Key][]string{
		MigrationStarted:               {ParamPath, ParamFrom, ParamTo, ParamFile},
		MigrationDone:                  {ParamPath, ParamTo},
		MigrationInstanceConfigured:    {ParamValue},
		MigrationInstanceGenerated:     {ParamValue},
		MigrationWorkspaceFromRepo:     {ParamPath},
		MigrationRepoDropped:           {ParamPath},
		MigrationGatewaysBound:         {ParamName, ParamKind},
		MigrationWorktreeRelocated:     {ParamName, ParamFrom, ParamTo},
		MigrationBranchRenamed:         {ParamName, ParamFrom, ParamTo},
		MigrationNewerFormat:           {ParamPath, ParamHave, ParamWant},
		MigrationPending:               {ParamPath},
		MigrationNoGateway:             {ParamName},
		MigrationRemoteWorktree:        {ParamName, ParamHost, ParamRepo, ParamFrom, ParamTo, ParamWhere, ParamBranch, ParamTarget},
		MigrationRemoteScratch:         {ParamName, ParamHost, ParamFrom, ParamTo, ParamWhere},
		MigrationConversationReset:     {ParamName, ParamPath},
		MigrationBackupFailed:          {ParamPath, ParamErr},
		MigrationRefused:               {ParamErr},
		MigrationCheckpointsRekeyed:    {ParamName, ParamCount, ParamFrom, ParamTo},
		MigrationUndoRekeyed:           {ParamName, ParamFrom, ParamTo},
		MigrationRekeyFinished:         {ParamName, ParamCount},
		MigrationCheckpointsRenumbered: {ParamName, ParamCount, ParamFrom, ParamTo, ParamStart},
		MigrationUndoReplaced:          {ParamName, ParamFrom, ParamTo},
		MigrationCheckpointInspect:     {ParamName, ParamPath, ParamErr, ParamCommand, ParamTrust, ParamRecovery},
		MigrationCheckpointHidden:      {ParamName, ParamPath, ParamRepo, ParamErr},
		MigrationCheckpointMove:        {ParamName, ParamPath, ParamErr},
		MigrationSharedWorktree:        {ParamName, ParamTarget, ParamPath, ParamFile},
		MigrationInterrupted:           {ParamErr, ParamDetail, ParamFile},
		MigrationNothingDone:           {},
		InstanceGenerated:              {ParamValue},
		InstanceStoredInvalid:          {ParamPath, ParamValue},
	}
}
