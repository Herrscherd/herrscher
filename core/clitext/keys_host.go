package clitext

// Keys of the replies that host commands return to the operator.
const (
	HostHomeSet               = "cli.host.home.set"
	HostSourceSet             = "cli.host.source.set"
	HostAgentCreated          = "cli.host.agent.created"
	HostAgentListEmpty        = "cli.host.agent.list_empty"
	HostAgentSoulUpdated      = "cli.host.agent.soul_updated"
	HostServiceRestarting     = "cli.host.service.restarting"
	HostServiceRebuilt        = "cli.host.service.rebuilt"
	HostSessionStartedPost    = "cli.host.session.started_post"
	HostCheckpointNone        = "cli.host.checkpoint.none"
	HostCheckpointUndone      = "cli.host.checkpoint.undone"
	HostMemoryRestored        = "cli.host.memory.restored"
	HostMemoryUnlinked        = "cli.host.memory.unlinked"
	HostScheduleNone          = "cli.host.schedule.none"
	HostScheduleRemoved       = "cli.host.schedule.removed"
	HostScheduleFired         = "cli.host.schedule.fired"
	HostSchedulePaused        = "cli.host.schedule.paused"
	HostScheduleResumed       = "cli.host.schedule.resumed"
	HostApproveNone           = "cli.host.approve.none"
	HostApproveAllowed        = "cli.host.approve.allowed"
	HostApproveDenied         = "cli.host.approve.denied"
	HostApproveRuleRemoved    = "cli.host.approve.rule_removed"
	HostApproveRuleAdded      = "cli.host.approve.rule_added"
	HostApproveRuleNone       = "cli.host.approve.rule_none"
	HostModelsNone            = "cli.host.models.none"
	HostModelsHidden          = "cli.host.models.hidden"
	HostModelsRefreshNone     = "cli.host.models.refresh_none"
	HostModelsDiscovered      = "cli.host.models.discovered"
	HostModelsDiscoveryKept   = "cli.host.models.discovery_kept"
	HostRoleNone              = "cli.host.role.none"
	HostRoleFirstGrant        = "cli.host.role.first_grant"
	HostRoleGrantable         = "cli.host.role.grantable"
	HostRoleAgentNote         = "cli.host.role.agent_note"
	HostRoleGranted           = "cli.host.role.granted"
	HostRoleRevokedLast       = "cli.host.role.revoked_last"
	HostRoleRevoked           = "cli.host.role.revoked"
	HostRoleHolds             = "cli.host.role.holds"
	HostModelsDiscoveryStatic = "cli.host.models.discovery_static"
)

func hostKeys() map[Key][]string {
	return map[Key][]string{
		HostHomeSet:               {ParamChannel, ParamKind},
		HostSourceSet:             {ParamPath},
		HostAgentCreated:          {ParamName, ParamHome},
		HostAgentListEmpty:        nil,
		HostAgentSoulUpdated:      {ParamName},
		HostServiceRestarting:     nil,
		HostServiceRebuilt:        {ParamVersion},
		HostSessionStartedPost:    {ParamName},
		HostCheckpointNone:        nil,
		HostCheckpointUndone:      {ParamName},
		HostMemoryRestored:        {ParamKey},
		HostMemoryUnlinked:        {ParamFrom, ParamTo},
		HostScheduleNone:          nil,
		HostScheduleRemoved:       {ParamName},
		HostScheduleFired:         {ParamName},
		HostSchedulePaused:        {ParamName},
		HostScheduleResumed:       {ParamName},
		HostApproveNone:           nil,
		HostApproveAllowed:        {ParamID},
		HostApproveDenied:         {ParamID},
		HostApproveRuleRemoved:    {ParamRule},
		HostApproveRuleAdded:      {ParamRule},
		HostApproveRuleNone:       nil,
		HostModelsNone:            nil,
		HostModelsHidden:          nil,
		HostModelsRefreshNone:     nil,
		HostModelsDiscovered:      {ParamCount},
		HostModelsDiscoveryKept:   {ParamErr},
		HostRoleNone:              nil,
		HostRoleFirstGrant:        nil,
		HostRoleGrantable:         {ParamRoles},
		HostRoleAgentNote:         nil,
		HostRoleGranted:           {ParamPrincipal, ParamRole},
		HostRoleRevokedLast:       {ParamPrincipal},
		HostRoleRevoked:           {ParamPrincipal},
		HostRoleHolds:             {ParamPrincipal, ParamRole},
		HostModelsDiscoveryStatic: {ParamErr},
	}
}
