package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSkillEngineConfigStates(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		write   bool
		wantNil bool
	}{
		{name: "malformed config disables skills", config: "{not json", write: true, wantNil: true},
		{name: "explicitly disabled", config: `{"skills":{"enabled":false}}`, write: true, wantNil: true},
		{name: "missing config keeps defaults", write: false, wantNil: false},
		{name: "well formed config keeps defaults", config: `{"skills":{}}`, write: true, wantNil: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HERRSCHER_STATE_DIR", dir)
			if tc.write {
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := newSkillEngine(nil)
			if (got == nil) != tc.wantNil {
				t.Fatalf("newSkillEngine nil=%v, want nil=%v", got == nil, tc.wantNil)
			}
		})
	}
}
