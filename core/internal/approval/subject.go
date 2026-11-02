package approval

var subjectKeys = map[string][]string{
	"Bash":         {"command"},
	"Edit":         {"file_path"},
	"Write":        {"file_path"},
	"Read":         {"file_path"},
	"NotebookEdit": {"notebook_path", "file_path"},
	"WebFetch":     {"url"},
	"WebSearch":    {"query"},
}

// SubjectOf pulls the part of a tool call that rules match against.
func SubjectOf(tool string, input map[string]any) string {
	for _, k := range subjectKeys[tool] {
		if s, ok := input[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
