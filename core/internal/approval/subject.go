package approval

// VerbTool is the tool name a daemon verb carries when a session asks the operator to run it, as in `allow Herrscher(schedule list*)`.
const VerbTool = "Herrscher"

const (
	keyFilePath   = "file_path"
	toolBash      = "Bash"
	toolRead      = "Read"
	toolEdit      = "Edit"
	toolWrite     = "Write"
	toolWebFetch  = "WebFetch"
	toolWebSearch = "WebSearch"
	toolGrep      = "Grep"
	toolLS        = "LS"
)

// CanonicalTool returns the name the approval rules use for a tool: a Grok
// Build tool name becomes its Claude Code alias, any other name is kept.
func CanonicalTool(tool string) string {
	if canonical, isGrok := grokAliases()[tool]; isGrok {
		return canonical
	}
	return tool
}

func grokAliases() map[string]string {
	return map[string]string{
		"run_terminal_command": toolBash,
		"read_file":            toolRead,
		"hashline_read":        toolRead,
		"search_replace":       toolEdit,
		"hashline_edit":        toolEdit,
		"write":                toolWrite,
		"grep":                 toolGrep,
		"hashline_grep":        toolGrep,
		"list_dir":             toolLS,
		"todo_write":           "TodoWrite",
		"spawn_subagent":       "Task",
		"web_fetch":            toolWebFetch,
		"web_search":           toolWebSearch,
	}
}

func subjectKeys(tool string) []string {
	switch tool {
	case toolBash:
		return []string{"command"}
	case toolEdit, toolWrite:
		return []string{keyFilePath}
	case toolRead:
		return []string{keyFilePath, "target_file"}
	case "NotebookEdit":
		return []string{"notebook_path", keyFilePath}
	case toolWebFetch:
		return []string{"url"}
	case toolWebSearch:
		return []string{"query"}
	}
	return nil
}

// SubjectOf pulls the part of a tool call that rules match against.
func SubjectOf(tool string, input map[string]any) string {
	for _, k := range subjectKeys(tool) {
		if s, ok := input[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
