package clitext

import "github.com/Herrscherd/herrscher/core/termpaint"

// Printable returns a copy of params whose string values are escaped for the terminal, other values are kept.
func Printable(params Params) Params {
	if params == nil {
		return nil
	}
	escaped := make(Params, len(params))
	for name, value := range params {
		if text, ok := value.(string); ok {
			value = termpaint.Escape(text).String()
		}
		escaped[name] = value
	}
	return escaped
}
