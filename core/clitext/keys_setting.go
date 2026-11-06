package clitext

// Keys of the Help text of each terminal launch setting.
const (
	SettingTerminalLearn       = "cli.setting.terminal_learn"
	SettingTerminalExtractor   = "cli.setting.terminal_extractor"
	SettingTerminalConsolidate = "cli.setting.terminal_consolidate_every"
	SettingTerminalMemoryAgent = "cli.setting.terminal_memory_agent"
	SettingTerminalProject     = "cli.setting.terminal_project"
	SettingTerminalProjectPin  = "cli.setting.terminal_project_pin"
)

func settingKeys() map[Key][]string {
	return map[Key][]string{
		SettingTerminalLearn:       nil,
		SettingTerminalExtractor:   nil,
		SettingTerminalConsolidate: nil,
		SettingTerminalMemoryAgent: nil,
		SettingTerminalProject:     nil,
		SettingTerminalProjectPin:  nil,
	}
}
