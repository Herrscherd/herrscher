package approval

// subjectKeys names, per tool, the field of a tool call that a rule's pattern
// matches. A tool absent from this table has an empty subject, so only an empty
// pattern (or "*") can name it. That is deliberate: guessing a subject from an
// unknown tool's arguments would make a rule mean something nobody wrote.
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
