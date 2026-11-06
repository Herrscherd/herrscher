package clitext

// Names of the params of the worktree messages.
const (
	ParamRef      = "Ref"
	ParamTip      = "Tip"
	ParamRemove   = "Remove"
	ParamRecovery = "Recovery"
	ParamRoot     = "Root"
)

// Keys of the worktree package, rendered through worktree.FailureError.
const (
	WorktreeRemoteScratch            = "cli.worktree.remote_scratch"
	WorktreeRemoteMaterialize        = "cli.worktree.remote_materialize"
	WorktreeRemoteCall               = "cli.worktree.remote_call"
	WorktreeRemoteUnreadable         = "cli.worktree.remote_unreadable"
	WorktreeScratchNoRoot            = "cli.worktree.scratch_no_root"
	WorktreeScratchBadName           = "cli.worktree.scratch_bad_name"
	WorktreeScratchRemoveRefused     = "cli.worktree.scratch_remove_refused"
	WorktreeScratchCreate            = "cli.worktree.scratch_create"
	WorktreeScratchRemove            = "cli.worktree.scratch_remove"
	WorktreeFlagBase                 = "cli.worktree.flag_base"
	WorktreeNotAWorktree             = "cli.worktree.not_a_worktree"
	WorktreeOtherBranch              = "cli.worktree.other_branch"
	WorktreeUncommitted              = "cli.worktree.uncommitted"
	WorktreeUncommittedRemove        = "cli.worktree.uncommitted_remove"
	WorktreeAddFailed                = "cli.worktree.add_failed"
	WorktreePruneFailed              = "cli.worktree.prune_failed"
	WorktreeRemoveFailed             = "cli.worktree.remove_failed"
	WorktreeOrphanRecovery           = "cli.worktree.orphan_recovery"
	WorktreeOrphanRemove             = "cli.worktree.orphan_remove"
	WorktreeInspect                  = "cli.worktree.inspect"
	WorktreeBranchUnreadable         = "cli.worktree.branch_unreadable"
	WorktreeCleanUnverified          = "cli.worktree.clean_unverified"
	WorktreeBranchUnverified         = "cli.worktree.branch_unverified"
	WorktreeHeadUnreadable           = "cli.worktree.head_unreadable"
	WorktreeHeadUnreadableAfterMerge = "cli.worktree.head_unreadable_after_merge"
	WorktreeMergeFailed              = "cli.worktree.merge_failed"
	WorktreeListFailed               = "cli.worktree.list_failed"
	WorktreeResolveTarget            = "cli.worktree.resolve_target"
	WorktreeResolveRegistered        = "cli.worktree.resolve_registered"
)

func worktreeKeys() map[Key][]string {
	return map[Key][]string{
		WorktreeRemoteScratch:            {ParamWhere},
		WorktreeRemoteMaterialize:        {ParamWhere, ParamErr},
		WorktreeRemoteCall:               {ParamCommand, ParamWhere, ParamErr},
		WorktreeRemoteUnreadable:         {ParamCommand, ParamWhere, ParamOutput, ParamErr},
		WorktreeScratchNoRoot:            {ParamName},
		WorktreeScratchBadName:           {ParamName},
		WorktreeScratchRemoveRefused:     {ParamName, ParamRoot},
		WorktreeScratchCreate:            {ParamPath, ParamErr},
		WorktreeScratchRemove:            {ParamPath, ParamErr},
		WorktreeFlagBase:                 {ParamRef},
		WorktreeNotAWorktree:             {ParamPath},
		WorktreeOtherBranch:              {ParamPath, ParamTip, ParamBranch},
		WorktreeUncommitted:              {ParamName},
		WorktreeUncommittedRemove:        {ParamName, ParamErr},
		WorktreeAddFailed:                {ParamOutput, ParamErr},
		WorktreePruneFailed:              {ParamOutput, ParamErr},
		WorktreeRemoveFailed:             {ParamOutput, ParamErr},
		WorktreeOrphanRecovery:           {ParamRemove, ParamRecovery},
		WorktreeOrphanRemove:             {ParamPath, ParamErr},
		WorktreeInspect:                  {ParamPath, ParamErr},
		WorktreeBranchUnreadable:         {ParamPath, ParamErr},
		WorktreeCleanUnverified:          {ParamPath, ParamErr},
		WorktreeBranchUnverified:         {ParamBranch, ParamPath, ParamErr},
		WorktreeHeadUnreadable:           {ParamPath, ParamErr},
		WorktreeHeadUnreadableAfterMerge: {ParamPath, ParamErr},
		WorktreeMergeFailed:              {ParamBranch, ParamPath, ParamOutput, ParamErr},
		WorktreeListFailed:               {ParamOutput, ParamErr},
		WorktreeResolveTarget:            {ParamPath, ParamErr},
		WorktreeResolveRegistered:        {ParamErr},
	}
}
