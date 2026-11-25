package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func exitFailure() error {
	return exec.CommandContext(context.Background(), "sh", "-c", "exit 3").Run()
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
		err  func() error
		want string
	}{
		{
			name: "dataset missing",
			err: func() error {
				_, _, err := loadLongMemEval(missing, 10, 1, 6)
				return err
			},
			want: "jeu LongMemEval introuvable : " + missing,
		},
		{
			name: "dataset denied",
			err: func() error {
				return datasetRead(missing, &fs.PathError{Op: "open", Path: missing, Err: fs.ErrPermission})
			},
			want: "accès refusé au jeu LongMemEval : " + missing,
		},
		{
			name: "dataset unreadable",
			err: func() error {
				return datasetRead(missing, errors.New("input/output error"))
			},
			want: "lecture du jeu LongMemEval impossible : " + missing,
		},
		{
			name: "herrscher absent from PATH",
			err: func() error {
				_, err := parseConfig(budgetArgs("-herrscher", missing), nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "binaire herrscher " + missing + " introuvable dans PATH",
		},
		{
			name: "herrscher unusable",
			err: func() error {
				_, err := parseConfig(budgetArgs("-herrscher", plain), nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "binaire herrscher " + plain + " inutilisable",
		},
		{
			name: "flags syntax",
			err: func() error {
				_, err := parseConfig([]string{"-no-such-flag"}, nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "arguments de la ligne de commande invalides : voir --help",
		},
		{
			name: "credentials absent",
			err: func() error {
				_, err := parseConfig(budgetArgs("-credentials", missing), nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "fichier d'identifiants introuvable : " + missing,
		},
		{
			name: "credentials unreadable",
			err: func() error {
				_, err := parseConfig(budgetArgs("-credentials", filepath.Join(plain, "child")), nil, loadCatalogue(), i18n.English, noEnv)
				return err
			},
			want: "fichier d'identifiants illisible : " + filepath.Join(plain, "child"),
		},
		{
			name: "run timed out",
			err: func() error {
				return runFailure("arm", fmt.Errorf("%w: stderr", context.DeadlineExceeded))
			},
			want: "échec de l'exécution arm : délai dépassé",
		},
		{
			name: "run exited",
			err: func() error {
				return runFailure("arm", exitFailure())
			},
			want: "échec de l'exécution arm : code de sortie 3",
		},
		{
			name: "run unclassified",
			err: func() error {
				return runFailure("arm", errors.New("pipe closed"))
			},
			want: "échec de l'exécution arm",
		},
	}
	msgs := loadCatalogue()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := failureText(msgs, i18n.French, tc.err()); got != tc.want {
				t.Errorf("french text %q, want %q", got, tc.want)
			}
		})
	}
}

func budgetArgs(extra ...string) []string {
	return append([]string{"-budget-tokens", "10", "-reserve-tokens", "5"}, extra...)
}
