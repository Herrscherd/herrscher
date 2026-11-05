package clitext

// Keys of the skills messages printed while serving.
const (
	SkillsRetireFailed   = "cli.skills.retire_failed"
	SkillRemoved         = "cli.skills.removed"
	SkillRetiredDiverged = "cli.skills.retired_diverged"
	SkillRetiredLinked   = "cli.skills.retired_linked"
	SkillInstalled       = "cli.skills.installed"
	SkillUpdated         = "cli.skills.updated"
	SkillDiverged        = "cli.skills.diverged"
	SkillLinked          = "cli.skills.linked"
	SkillsNotConfigured  = "cli.skills.not_configured"
	SkillsPluginFailed   = "cli.skills.plugin_failed"
	SkillsNoContent      = "cli.skills.no_content"
	SkillsInstallFailed  = "cli.skills.install_failed"
	SkillDuplicate       = "cli.skills.duplicate"
)

func serveKeys() map[Key][]string {
	return map[Key][]string{
		SkillsRetireFailed:   {ParamErr},
		SkillRemoved:         {ParamName},
		SkillRetiredDiverged: {ParamName, ParamPath},
		SkillRetiredLinked:   {ParamName, ParamPath},
		SkillInstalled:       {ParamName},
		SkillUpdated:         {ParamName},
		SkillDiverged:        {ParamName, ParamPath},
		SkillLinked:          {ParamName, ParamPath},
		SkillsNotConfigured:  {ParamKind, ParamErr},
		SkillsPluginFailed:   {ParamKind, ParamErr},
		SkillsNoContent:      {ParamKind},
		SkillsInstallFailed:  {ParamKind, ParamErr},
		SkillDuplicate:       {ParamName, ParamKind, ParamOwner},
	}
}
