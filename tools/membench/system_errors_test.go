package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func exitFailure() error {
	return exec.CommandContext(context.Background(), "sh", "-c", "exit 3").Run()
}

func killedFailure() error {
	return exec.CommandContext(context.Background(), "sh", "-c", "kill -KILL $$").Run()
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("disk full")
}

func TestSystemErrorsReadInFrench(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent")
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		err  func(*testing.T) error
		want string
	}{
		{
			name: "dataset missing",
			err: func(*testing.T) error {
				_, _, err := loadLongMemEval(missing, 10, 1, 6)
				return err
			},
			want: "jeu LongMemEval introuvable : " + missing,
		},
		{
			name: "dataset denied",
			err: func(*testing.T) error {
				return datasetRead(missing, &fs.PathError{Op: "open", Path: missing, Err: fs.ErrPermission})
			},
			want: "accès refusé au jeu LongMemEval : " + missing,
		},
		{
			name: "dataset unreadable",
			err: func(*testing.T) error {
				return datasetRead(missing, errors.New("input/output error"))
			},
			want: "lecture du jeu LongMemEval impossible : " + missing,
		},
		{
			name: "herrscher absent from PATH",
			err: func(*testing.T) error {
				_, err := parseConfig(budgetArgs("-herrscher", missing), nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "binaire herrscher " + missing + " introuvable dans PATH",
		},
		{
			name: "herrscher unusable",
			err: func(*testing.T) error {
				_, err := parseConfig(budgetArgs("-herrscher", plain), nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "binaire herrscher " + plain + " inutilisable",
		},
		{
			name: "flags syntax",
			err: func(*testing.T) error {
				_, err := parseConfig([]string{"-no-such-flag"}, nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "arguments de la ligne de commande invalides : voir --help",
		},
		{
			name: "credentials absent",
			err: func(*testing.T) error {
				_, err := parseConfig(budgetArgs("-credentials", missing), nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "fichier d'identifiants introuvable : " + missing,
		},
		{
			name: "credentials unreadable",
			err: func(*testing.T) error {
				_, err := parseConfig(budgetArgs("-credentials", filepath.Join(plain, "child")), nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "fichier d'identifiants illisible : " + filepath.Join(plain, "child"),
		},
		{
			name: "run timed out",
			err: func(t *testing.T) error {
				t.Helper()
				return timedOutRun(t)
			},
			want: "échec de l'exécution arm : délai dépassé",
		},
		{
			name: "run exited",
			err: func(*testing.T) error {
				return runFailure("arm", exitFailure())
			},
			want: "échec de l'exécution arm : code de sortie 3",
		},
		{
			name: "run unclassified",
			err: func(*testing.T) error {
				return runFailure("arm", errors.New("pipe closed"))
			},
			want: "échec de l'exécution arm",
		},
		{
			name: "run killed by signal",
			err: func(*testing.T) error {
				return runFailure("arm", killedFailure())
			},
			want: "échec de l'exécution arm : tué par le signal 9",
		},
		{
			name: "cache directory unusable",
			err: func(t *testing.T) error {
				t.Helper()
				t.Setenv("HOME", "")
				t.Setenv("XDG_CACHE_HOME", "")
				_, err := benchParent()
				return err
			},
			want: "répertoire de cache de membench inutilisable",
		},
		{
			name: "cache directory is a file",
			err: func(t *testing.T) error {
				t.Helper()
				t.Setenv("XDG_CACHE_HOME", plain)
				_, err := benchParent()
				return err
			},
			want: "répertoire de cache de membench inutilisable : " + filepath.Join(plain, benchDirName),
		},
		{
			name: "sandbox parent missing",
			err: func(*testing.T) error {
				_, err := newSandbox(missing)
				return err
			},
			want: "bac à sable de membench impossible à préparer ou à nettoyer",
		},
		{
			name: "vault cannot be planted",
			err: func(*testing.T) error {
				return sandbox{root: plain}.plantAgedNode()
			},
			want: "coffre de membench impossible à préparer",
		},
		{
			name: "credentials copy unreadable",
			err: func(*testing.T) error {
				return sandbox{config: dir}.installCredentials(missing)
			},
			want: "fichier d'identifiants illisible : " + missing,
		},
		{
			name: "orphan sweep cannot list",
			err: func(*testing.T) error {
				return sweepOrphans(plain)
			},
			want: "nettoyage des exécutions orphelines de membench impossible",
		},
		{
			name: "markdown report cannot be written",
			err: func(*testing.T) error {
				return writeReport(failingWriter{}, report{Status: statusComplete}, config{format: formatMarkdown, lang: i18n.French}, loadCatalogue())
			},
			want: "écriture du rapport de membench impossible",
		},
		{
			name: "json report cannot be written",
			err: func(*testing.T) error {
				return writeReport(failingWriter{}, report{Status: statusComplete}, config{format: formatJSON, lang: i18n.French}, loadCatalogue())
			},
			want: "écriture du rapport de membench impossible",
		},
		{
			name: "dataset is a directory",
			err: func(*testing.T) error {
				_, _, err := loadLongMemEval(dir, 10, 1, 6)
				return err
			},
			want: "lecture du jeu LongMemEval impossible : " + dir,
		},
		{
			name: "unclassified system error",
			err: func(*testing.T) error {
				return errors.New("boom")
			},
			want: "membench a rencontré une erreur système non classée",
		},
	}
	msgs := loadCatalogue()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := failureText(msgs, i18n.French, tc.err(t)); got != tc.want {
				t.Errorf("french text %q, want %q", got, tc.want)
			}
		})
	}
}

func budgetArgs(extra ...string) []string {
	return append([]string{"-budget-tokens", "10", "-reserve-tokens", "5"}, extra...)
}

func timedOutRun(t *testing.T) error {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r := runner{binary: binary, model: defaultModel}
	_, invokeErr := r.invoke(ctx, box, step{agent: agentAlpha, prompt: "x"}, nil)
	return runFailure("arm", invokeErr)
}

func TestDetailNeverLeaksSecretAcrossTheCutoff(t *testing.T) {
	const secret = "placeholder-key"
	binary := filepath.Join(t.TempDir(), "fake")
	padding := strings.Repeat("a", maxDetailBytes-len(secret)/2)
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s%s' >&2\nexit 3\n", padding, secret)
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner{binary: binary, model: defaultModel, apiKey: secret}
	_, invokeErr := r.invoke(context.Background(), box, step{agent: agentAlpha, prompt: "x"}, nil)
	got := failureText(loadCatalogue(), i18n.French, runFailure("arm", invokeErr))
	if fragment := secret[:len(secret)/2]; strings.Contains(got, fragment) {
		t.Errorf("failure text %q keeps the secret fragment %q", got, fragment)
	}
}

func TestUnusableCacheStillWritesFailedStatus(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	binary := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	msgs := loadCatalogue()
	cfg, err := parseConfig(budgetArgs("-herrscher", binary), nil, msgs, i18n.English, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	rep, runErr := execute(t.Context(), cfg, nil, 0, msgs)
	if runErr == nil {
		t.Fatal("execute returned no error without a cache directory")
	}
	if rep.Status != statusFailed {
		t.Errorf("status %q, want %q", rep.Status, statusFailed)
	}
	if rep.Failure == "" {
		t.Error("the report carries no failure text")
	}
}

func TestRunFailureKeepsRedactedStderr(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake")
	script := "#!/bin/sh\necho 'boom: quota detail placeholder-key' >&2\nexit 3\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner{binary: binary, model: defaultModel, apiKey: "placeholder-key"}
	_, invokeErr := r.invoke(context.Background(), box, step{agent: agentAlpha, prompt: "x"}, nil)
	got := failureText(loadCatalogue(), i18n.French, runFailure("arm", invokeErr))
	want := "échec de l'exécution arm : code de sortie 3, sortie d'erreur : boom: quota detail [redacted]"
	if got != want {
		t.Errorf("french text %q, want %q", got, want)
	}
}

func TestDetailTextCutsOnRuneBoundary(t *testing.T) {
	stderr := strings.Repeat("a", maxDetailBytes-1) + "é"
	got := detailText(stderr)
	if want := strings.Repeat("a", maxDetailBytes-1); got != want {
		t.Errorf("detail is %d bytes, want %d", len(got), len(want))
	}
	if !utf8.ValidString(got) {
		t.Errorf("detail %q is not valid UTF-8", got)
	}
}

func TestHerrscherCauseSurvivesErrorsIs(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := parseConfig(budgetArgs("-herrscher", plain), nil, loadCatalogue(), i18n.English, noEnv)
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("error %v does not keep the permission cause", err)
	}
}

func TestFailedReportLineIsFrench(t *testing.T) {
	msgs := loadCatalogue()
	rep := report{Status: statusFailed, Failure: failureText(msgs, i18n.French, errors.New("boom"))}
	var out strings.Builder
	if err := writeReport(&out, rep, config{format: formatMarkdown, lang: i18n.French}, msgs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "membench a rencontré une erreur système non classée") {
		t.Errorf("report %q lacks the French fallback", out.String())
	}
	if strings.Contains(out.String(), "boom") {
		t.Errorf("report %q leaks the English cause", out.String())
	}
}
