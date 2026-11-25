package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/skills"
)

func TestSeedBackendCarriesTheSessionIdentity(t *testing.T) {
	settings := make(map[string]string)
	applyBackendRequest(settings, BackendRequest{Session: "first"}, modelRoute{})
	want := contracts.EncodeEnvSetting(map[string]string{control.SessionVar: "first"})
	if settings["env"] != want {
		t.Fatalf("env setting %q, want %q", settings["env"], want)
	}
}

func TestSeedBackendCarriesTheSessionToken(t *testing.T) {
	settings := make(map[string]string)
	applyBackendRequest(settings, BackendRequest{Session: "first", SessionToken: "tok-first"}, modelRoute{})
	want := contracts.EncodeEnvSetting(map[string]string{control.SessionVar: "first", control.ApprovalKeyVar: "tok-first"})
	if settings["env"] != want {
		t.Fatalf("env setting %q, want the session and its token %q", settings["env"], want)
	}
}

func TestSeedBackendNamesTheLearnedSkillHomeOfItsDirectory(t *testing.T) {
	dir := t.TempDir()
	settings := make(map[string]string)
	applyBackendRequest(settings, BackendRequest{SkillRoot: learnedHomeOf(seedBackendDir(state.Session{Worktree: dir}))}, modelRoute{})
	if got, want := settings["skill-root"], skills.LearnedHome(dir); got != want {
		t.Fatalf("skill-root setting %q, want %q so the vendor loads the skills projected in the worktree", got, want)
	}
}

func TestSeedBackendWithoutADirectoryNamesNoSkillRoot(t *testing.T) {
	settings := make(map[string]string)
	applyBackendRequest(settings, BackendRequest{SkillRoot: learnedHomeOf(seedBackendDir(state.Session{}))}, modelRoute{})
	if got, ok := settings["skill-root"]; ok {
		t.Fatalf("skill-root setting %q with no session directory", got)
	}
}
