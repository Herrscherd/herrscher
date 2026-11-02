package approval

const (
	keyFilePath = "file_path"
)

func subjectKeys(tool string) []string {
	switch tool {
	case "Bash":
		return []string{"command"}
	case "Edit", "Write", "Read":
		return []string{keyFilePath}
	case "NotebookEdit":
		return []string{"notebook_path", keyFilePath}
	case "WebFetch":
		return []string{"url"}
	case "WebSearch":
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
