package approval

const (
	keyFilePath = "file_path"
)

var subjectKeys = map[string][]string{
	"Bash":         {"command"},
	"Edit":         {keyFilePath},
	"Write":        {keyFilePath},
	"Read":         {keyFilePath},
	"NotebookEdit": {"notebook_path", keyFilePath},
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
