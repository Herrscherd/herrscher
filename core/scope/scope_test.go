package scope

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("commit", "-q", "--allow-empty", "-m", "root")
	return dir
}

func TestProjectFromDirNamesTheRepository(t *testing.T) {
	repo := gitRepo(t, "Herrscher")
	if got := ProjectFromDir(repo); got != "herrscher" {
		t.Fatalf("ProjectFromDir(repo) = %q, want %q", got, "herrscher")
	}
}

func TestProjectFromDirAnswersTheSameFromASubdirectory(t *testing.T) {
	repo := gitRepo(t, "herrscher")
	sub := filepath.Join(repo, "core", "scope")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ProjectFromDir(sub); got != "herrscher" {
		t.Fatalf("ProjectFromDir(sub) = %q, want %q", got, "herrscher")
	}
}

func TestProjectFromDirFoldsWorktreesIntoTheirRepository(t *testing.T) {
	repo := gitRepo(t, "herrscher")
	wt := filepath.Join(filepath.Dir(repo), "detached-elsewhere")
	cmd := exec.CommandContext(t.Context(), "git", "-C", repo, "worktree", "add", "-q", "-b", "side", wt)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	if got := ProjectFromDir(wt); got != "herrscher" {
		t.Fatalf("ProjectFromDir(worktree) = %q, want %q", got, "herrscher")
	}
}

func TestProjectFromDirIsSilentOutsideARepository(t *testing.T) {
	if got := ProjectFromDir(t.TempDir()); got != "" {
		t.Fatalf("ProjectFromDir(non-repo) = %q, want empty", got)
	}
	if got := ProjectFromDir(""); got != "" {
		t.Fatalf("ProjectFromDir(\"\") = %q, want empty", got)
	}
}

func TestProjectFromDirRefusesAnUnnameableDirectory(t *testing.T) {
	if got := ProjectFromDir(gitRepo(t, "...")); got != "" {
		t.Fatalf("ProjectFromDir(unnameable) = %q, want empty", got)
	}
}

func TestMatchProject(t *testing.T) {
	known := []string{"herrscher", "neublox", "herrscher-docs"}
	for _, tc := range []struct {
		name, prompt, want string
	}{
		{"names one", "je bosse sur neublox aujourd'hui", "neublox"},
		{"names none", "on continue la ou on en etait", ""},
		{"case folds", "NEUBLOX est casse", "neublox"},
		{"earliest named wins", "neublox puis herrscher", "neublox"},
		{"whole words only", "neubloxide est un autre projet", ""},
		{"longest at the same place wins", "herrscher-docs a besoin d'une page", "herrscher-docs"},
		{"nothing known, nothing matched", "neublox", ""},
		{"typographic punctuation is a boundary", "cf. neublox\u2026", "neublox"},
		{"a non-breaking space is a boundary", "\u00ab\u00a0neublox\u00a0\u00bb", "neublox"},
		{"an accent is not a boundary", "neubloxe\u0301 n'est pas neublox", "neublox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := known
			if tc.name == "nothing known, nothing matched" {
				k = nil
			}
			if got := MatchProject(tc.prompt, k); got != tc.want {
				t.Fatalf("MatchProject(%q) = %q, want %q", tc.prompt, got, tc.want)
			}
		})
	}
}
