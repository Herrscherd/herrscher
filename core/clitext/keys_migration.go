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
	MigrationRemoteSession      = "cli.migration.remote_session"
	MigrationRefused            = "cli.migration.refused"
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
		MigrationRemoteSession:      {ParamName, ParamHost, ParamPath},
		MigrationRefused:            {ParamErr},
		InstanceGenerated:           {ParamValue},
	}
}
