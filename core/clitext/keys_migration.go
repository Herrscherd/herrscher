package clitext

// Keys of the state format migration, announced in the daemon log and on the output of serve.
const (
	MigrationStarted            = "cli.migration.started"
	MigrationDone               = "cli.migration.done"
	MigrationInstanceConfigured = "cli.migration.instance_configured"
	MigrationInstanceGenerated  = "cli.migration.instance_generated"
	MigrationWorkspaceFromRepo  = "cli.migration.workspace_from_repo"
	MigrationRepoDropped        = "cli.migration.repo_dropped"
	MigrationGatewaysBound      = "cli.migration.gateways_bound"
	MigrationWorktreeRelocated  = "cli.migration.worktree_relocated"
	MigrationBranchRenamed      = "cli.migration.branch_renamed"
	MigrationNewerFormat        = "cli.migration.newer_format"
	MigrationPending            = "cli.migration.pending"
	MigrationNoGateway          = "cli.migration.no_gateway"
	MigrationRemoteWorktree     = "cli.migration.remote_worktree"
	MigrationRemoteScratch      = "cli.migration.remote_scratch"
	MigrationConversationReset  = "cli.migration.conversation_reset"
	MigrationBackupFailed       = "cli.migration.backup_failed"
	MigrationRefused            = "cli.migration.refused"
	MigrationCheckpointsRekeyed = "cli.migration.checkpoints_rekeyed"
	MigrationUndoRekeyed        = "cli.migration.undo_rekeyed"
	MigrationRekeyFinished      = "cli.migration.rekey_finished"
	MigrationCheckpointConflict = "cli.migration.checkpoint_conflict"
	MigrationCheckpointInspect  = "cli.migration.checkpoint_inspect"
	MigrationCheckpointMove     = "cli.migration.checkpoint_move"
	InstanceGenerated           = "cli.instance.generated"
)

func migrationKeys() map[Key][]string {
	return map[Key][]string{
		MigrationStarted:            {ParamPath, ParamFrom, ParamTo, ParamFile},
		MigrationDone:               {ParamPath, ParamTo},
		MigrationInstanceConfigured: {ParamValue},
		MigrationInstanceGenerated:  {ParamValue},
		MigrationWorkspaceFromRepo:  {ParamPath},
		MigrationRepoDropped:        {ParamPath},
		MigrationGatewaysBound:      {ParamName, ParamKind},
		MigrationWorktreeRelocated:  {ParamName, ParamFrom, ParamTo},
		MigrationBranchRenamed:      {ParamName, ParamFrom, ParamTo},
		MigrationNewerFormat:        {ParamPath, ParamHave, ParamWant},
		MigrationPending:            {ParamPath},
		MigrationNoGateway:          {ParamName},
		MigrationRemoteWorktree:     {ParamName, ParamHost, ParamRepo, ParamFrom, ParamTo, ParamWhere, ParamBranch, ParamTarget},
		MigrationRemoteScratch:      {ParamName, ParamHost, ParamFrom, ParamTo, ParamWhere},
		MigrationConversationReset:  {ParamName, ParamPath},
		MigrationBackupFailed:       {ParamPath, ParamErr},
		MigrationRefused:            {ParamErr},
		MigrationCheckpointsRekeyed: {ParamName, ParamCount, ParamFrom, ParamTo},
		MigrationUndoRekeyed:        {ParamName, ParamFrom, ParamTo},
		MigrationRekeyFinished:      {ParamName, ParamCount},
		MigrationCheckpointConflict: {ParamName, ParamRef, ParamHave, ParamWant, ParamPath},
		MigrationCheckpointInspect:  {ParamName, ParamPath, ParamErr},
		MigrationCheckpointMove:     {ParamName, ParamPath, ParamErr},
		InstanceGenerated:           {ParamValue},
	}
}
