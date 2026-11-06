package host

import (
	"archive/tar"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
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
