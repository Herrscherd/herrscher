package identity

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepo makes a real repository under t.TempDir with no identity configured
// and returns its path, with the developer's own global config walled off.
// Real git, because the whole point of FromDir is what git answers.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestFromDirReadsTheRepositoryConfig(t *testing.T) {
	isolate(t)
	repo := gitRepo(t)
	git(t, repo, "config", "user.name", "Jane Doe")
	git(t, repo, "config", "user.email", "jane@example.com")
	git(t, repo, "config", "github.user", "janedoe")

	got := FromDir(repo)
	want := Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"}
	if got != want {
		t.Fatalf("FromDir = %+v, want %+v", got, want)
	}
}

func TestFromDirLeavesUnsetKeysEmpty(t *testing.T) {
	isolate(t)
	repo := gitRepo(t)
	git(t, repo, "config", "user.name", "Jane Doe")

	got := FromDir(repo)
	want := Identity{Name: "Jane Doe"}
	if got != want {
		t.Fatalf("FromDir = %+v, want %+v", got, want)
	}
}

// A repository with nothing configured is a silence, not a failure: the zero
// Identity is what every caller renders as "git had nothing to say".
// isolate walls off the developer's own git config for the tests that assert a
// silence. Without it they would assert against whoever ran them: git answers
// `config --get` from the global file even outside a repository, so "no local
// identity" is not the same question as "no identity at all".
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
}

// A repository with nothing configured, on a machine with nothing configured,
// is a silence rather than a failure: the zero Identity is what every caller
// renders as "git had nothing to say".
func TestFromDirIsSilentWhenNothingIsConfigured(t *testing.T) {
	isolate(t)
	if got := FromDir(gitRepo(t)); !got.Empty() {
		t.Fatalf("FromDir = %+v, want the zero Identity", got)
	}
}

func TestFromDirIsSilentOutsideARepositoryWithNoGlobalConfig(t *testing.T) {
	isolate(t)
	if got := FromDir(t.TempDir()); !got.Empty() {
		t.Fatalf("FromDir = %+v, want the zero Identity", got)
	}
}

// Outside a repository git still answers, from the global config — and that is
// the answer we want: a session started in a directory that is no repository
// still knows who its human is.
func TestFromDirFallsBackToTheGlobalConfigOutsideARepository(t *testing.T) {
	home := t.TempDir()
	global := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(global, []byte("[user]\n\tname = Jane Doe\n\temail = jane@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	got := FromDir(t.TempDir())
	want := Identity{Name: "Jane Doe", Email: "jane@example.com"}
	if got != want {
		t.Fatalf("FromDir = %+v, want %+v", got, want)
	}
}

// The local value wins over the global one, because git decides precedence and
// we only ask. This is what lets a per-repository identity work for free.
func TestFromDirPrefersTheRepositoryConfigOverTheGlobalOne(t *testing.T) {
	home := t.TempDir()
	global := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(global, []byte("[user]\n\tname = Global Name\n\temail = global@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	repo := gitRepo(t)
	git(t, repo, "config", "user.email", "local@example.com")

	got := FromDir(repo)
	want := Identity{Name: "Global Name", Email: "local@example.com"}
	if got != want {
		t.Fatalf("FromDir = %+v, want %+v", got, want)
	}
}

func TestStringOmitsWhatGitDidNotAnswer(t *testing.T) {
	cases := []struct {
		name string
		id   Identity
		want string
	}{
		{"full", Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"}, "Jane Doe <jane@example.com> (@janedoe)"},
		{"no github", Identity{Name: "Jane Doe", Email: "jane@example.com"}, "Jane Doe <jane@example.com>"},
		{"email only", Identity{Email: "jane@example.com"}, "<jane@example.com>"},
		{"github only", Identity{GitHub: "janedoe"}, "(@janedoe)"},
		{"empty", Identity{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.id.String(); got != c.want {
				t.Fatalf("String() = %q, want %q", got, c.want)
			}
		})
	}
}
