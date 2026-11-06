package clitext

import (
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestEveryWorktreeKeyRendersWithItsDeclaredFieldsInEachLanguage(t *testing.T) {
	for key, fields := range worktreeKeys() {
		params := Params{}
		for _, field := range fields {
			params[field] = "x"
		}
		for _, lang := range []i18n.Language{i18n.English, i18n.French} {
			text, err := Text(lang, key, params)
			if err != nil {
				t.Fatalf("%s in %v: %v", key, lang, err)
			}
			if text == "" || text == string(key) {
				t.Fatalf("%s in %v rendered %q", key, lang, text)
			}
		}
	}
}
