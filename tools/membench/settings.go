package main

import "strings"

const (
	settingOn  = "true"
	settingOff = "false"
)

type setting struct {
	name string
	env  string
}

func knownSettings() []setting {
	return []setting{
		{name: "learned-skills", env: "MEMORY_LEARNED_SKILLS"},
		{name: "raw-archive", env: "MEMORY_RAW_ARCHIVE"},
		{name: "report", env: "MEMORY_REPORT_ENABLED"},
	}
}

func settingNamed(name string) (setting, bool) {
	for _, known := range knownSettings() {
		if known.name == name {
			return known, true
		}
	}
	return setting{}, false
}

func selectSettings(list string) ([]setting, error) {
	if list == settingsAll {
		return knownSettings(), nil
	}
	var chosen []setting
	for _, name := range strings.Split(list, ",") {
		trimmed := strings.TrimSpace(name)
		found, ok := settingNamed(trimmed)
		if !ok {
			return nil, flagError("settings", trimmed)
		}
		chosen = append(chosen, found)
	}
	return chosen, nil
}

func (s setting) assignment(enabled bool) string {
	value := settingOff
	if enabled {
		value = settingOn
	}
	return s.env + "=" + value
}
