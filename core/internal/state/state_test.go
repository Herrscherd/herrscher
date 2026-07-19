package state

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadStateBackfillsSessionID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	legacy := `{"sessions":[{"name":"alpha","channelID":"123","type":"text","cmd":"claude"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := st.FindSession("alpha")
	if !ok {
		t.Fatal("session alpha missing after load")
	}
	if s.ID == "" {
		t.Fatalf("legacy session should get a generated ID")
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	s.Home = HomeRef{ID: "123", Type: "category"}
	s.Sessions = []Session{{Name: "foo", ChannelID: "c1", Type: "text", Cmd: "claude"}}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Home.ID != "123" || len(got.Sessions) != 1 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLoadStateMissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(s.Sessions) != 0 {
		t.Fatal("expected empty state")
	}
}

func TestSessionMutations(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "s.json"))
	if err := s.AddSession(Session{Name: "a", ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.FindSession("a"); !ok {
		t.Fatal("expected to find a")
	}
	if err := s.AddSession(Session{Name: "a"}); err == nil {
		t.Fatal("duplicate session name should error")
	}
	if err := s.RemoveSession("a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.FindSession("a"); ok {
		t.Fatal("a should be gone")
	}
}

func TestQualifiedName(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		logical    string
		want       string
	}{
		{"namespaced", "alice", "foo", "alice__foo"},
		{"legacy-empty-id", "", "foo", "foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewState(filepath.Join(t.TempDir(), "s.json"))
			s.InstanceID = tt.instanceID
			if got := s.QualifiedName(tt.logical); got != tt.want {
				t.Fatalf("QualifiedName(%q) with id %q = %q, want %q", tt.logical, tt.instanceID, got, tt.want)
			}
		})
	}
}

func TestSetInstanceIDPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	s := NewState(path)
	if err := s.SetInstanceID("alice"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.InstanceID != "alice" {
		t.Fatalf("InstanceID not persisted: %q", got.InstanceID)
	}
}

func TestSetWorkspacePersists(t *testing.T) {
	p := t.TempDir() + "/s.json"
	s := NewState(p)
	if err := s.SetWorkspace("/home/u/dev"); err != nil {
		t.Fatalf("SetWorkspace: %v", err)
	}
	reloaded, err := LoadState(p)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if reloaded.Workspace != "/home/u/dev" {
		t.Fatalf("workspace not persisted: %q", reloaded.Workspace)
	}
}

func TestWorkspaceRootFallsBack(t *testing.T) {
	s := NewState(t.TempDir() + "/s.json")
	if got := s.WorkspaceRoot(); got != "" {
		t.Fatalf("empty state should give empty root, got %q", got)
	}
	s.Repo = "/legacy/repo"
	if got := s.WorkspaceRoot(); got != "/legacy/repo" {
		t.Fatalf("should fall back to Repo, got %q", got)
	}
	_ = s.SetWorkspace("/ws")
	if got := s.WorkspaceRoot(); got != "/ws" {
		t.Fatalf("Workspace should win, got %q", got)
	}
}

func TestSessionProjectRoundTrips(t *testing.T) {
	p := t.TempDir() + "/s.json"
	s := NewState(p)
	if err := s.AddSession(Session{Name: "x", ChannelID: "c", Project: "myproj"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	reloaded, _ := LoadState(p)
	got, ok := reloaded.FindSession("x")
	if !ok || got.Project != "myproj" {
		t.Fatalf("project not persisted: %+v", got)
	}
}

func TestSessionLearningConfigRoundTrips(t *testing.T) {
	p := t.TempDir() + "/s.json"
	s := NewState(p)
	if err := s.AddSession(Session{
		Name: "x", ChannelID: "c",
		Extractor: "roblox", Journal: ".neublox/calls.log", ConsolidateEvery: 5,
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	reloaded, _ := LoadState(p)
	got, ok := reloaded.FindSession("x")
	if !ok || got.Extractor != "roblox" || got.Journal != ".neublox/calls.log" || got.ConsolidateEvery != 5 {
		t.Fatalf("learning config not persisted: %+v", got)
	}
}

func TestBoundGateways(t *testing.T) {
	cases := []struct {
		name string
		sess Session
		want []string
	}{
		{"explicit", Session{Gateways: []string{"terminal"}}, []string{"terminal"}},
		{"both", Session{Gateways: []string{"chat", "terminal"}}, []string{"chat", "terminal"}},
		{"legacy with channel is empty (host resolves)", Session{ChannelID: "c1"}, nil},
		{"empty no channel", Session{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.sess.BoundGateways()
			if !slices.Equal(got, tc.want) {
				t.Errorf("BoundGateways() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsLegacy(t *testing.T) {
	cases := []struct {
		name string
		sess Session
		want bool
	}{
		{"channel, no stored set", Session{ChannelID: "c1"}, true},
		{"channel with stored set", Session{ChannelID: "c1", Gateways: []string{"chat"}}, false},
		{"no channel", Session{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.sess.IsLegacy(); got != tc.want {
				t.Errorf("IsLegacy() = %v, want %v", got, tc.want)
			}
		})
	}
}
