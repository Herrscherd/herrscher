package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetBackendTargetReportsMatchAndPersistence(t *testing.T) {
	writable := filepath.Join(t.TempDir(), "state.json")
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		path      string
		session   string
		wantOK    bool
		wantError bool
	}{
		{name: "match persists", path: writable, session: "alpha", wantOK: true},
		{name: "unknown session", path: writable, session: "ghost"},
		{name: "persist failure surfaces", path: filepath.Join(blocker, "state.json"), session: "alpha", wantOK: true, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := NewState(tc.path)
			st.Sessions = append(st.Sessions, Session{Name: "alpha", Vendor: "claude", Cmd: "claude", ResumeToken: "tok"})
			ok, err := st.SetBackendTarget(tc.session, "codex", "codex", "gpt-5-codex")
			if ok != tc.wantOK {
				t.Fatalf("match = %v, want %v", ok, tc.wantOK)
			}
			if tc.wantError && err == nil {
				t.Fatal("a failed write must be reported")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
