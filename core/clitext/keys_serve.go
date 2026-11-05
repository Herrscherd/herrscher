package clitext

const (
	SkillsRetireFailed   Key = "cli.skills.retire_failed"
	SkillRemoved         Key = "cli.skills.removed"
	SkillRetiredDiverged Key = "cli.skills.retired_diverged"
	SkillRetiredLinked   Key = "cli.skills.retired_linked"
	SkillInstalled       Key = "cli.skills.installed"
	SkillUpdated         Key = "cli.skills.updated"
	SkillDiverged        Key = "cli.skills.diverged"
	SkillLinked          Key = "cli.skills.linked"
	SkillsNotConfigured  Key = "cli.skills.not_configured"
	SkillsPluginFailed   Key = "cli.skills.plugin_failed"
	SkillsNoContent      Key = "cli.skills.no_content"
	SkillsInstallFailed  Key = "cli.skills.install_failed"
	SkillDuplicate       Key = "cli.skills.duplicate"
)

var serveKeys = map[Key][]string{
	SkillsRetireFailed:   {"Err"},
	SkillRemoved:         {"Name"},
	SkillRetiredDiverged: {"Name", "Path"},
	SkillRetiredLinked:   {"Name", "Path"},
	SkillInstalled:       {"Name"},
	SkillUpdated:         {"Name"},
	SkillDiverged:        {"Name", "Path"},
	SkillLinked:          {"Name", "Path"},
	SkillsNotConfigured:  {"Kind", "Err"},
	SkillsPluginFailed:   {"Kind", "Err"},
	SkillsNoContent:      {"Kind"},
	SkillsInstallFailed:  {"Kind", "Err"},
	SkillDuplicate:       {"Name", "Kind", "Owner"},
}
