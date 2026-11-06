package host

import (
	"archive/tar"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

func TestArchiveRefusalReachesTheFrenchOperatorInFrench(t *testing.T) {
	dst := t.TempDir()
	var buf strings.Builder
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "../escaped", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	err := extractTar(strings.NewReader(buf.String()), dst)
	if err == nil {
		t.Fatal("want a refusal")
	}
	got := localizeHostError(i18n.French, err).Error()
	wantPrefix := "extraction de ../escaped impossible : élément d'archive refusé : ../escaped sortirait de "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("refusal = %q, want the french prefix %q", got, wantPrefix)
	}
	if strings.Contains(got, "refusing") || strings.Contains(got, "cannot extract") {
		t.Fatalf("english text reached the french operator: %q", got)
	}
}

func TestWorktreeFailureReachesTheFrenchOperatorInFrench(t *testing.T) {
	failure := worktree.FailureError{Key: clitext.WorktreeRemoteMaterialize, Params: clitext.Params{clitext.ParamWhere: "me@h", clitext.ParamErr: errors.New("boom")}}
	got := localizeHostError(i18n.French, fmt.Errorf("installer: %w", failure)).Error()
	if want := "installer: matérialisation sur me@h : boom"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if english := localizeHostError(i18n.English, failure).Error(); english != "materialize on me@h: boom" {
		t.Fatalf("english = %q", english)
	}
}
