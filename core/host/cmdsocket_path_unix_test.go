//go:build !windows

package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandSocketPathUnix(t *testing.T) {
	if got := CommandSocketPath(""); got != filepath.Join(os.TempDir(), "dctl-command.sock") {
		t.Fatalf("CommandSocketPath(\"\") = %q", got)
	}
	got := CommandSocketPath("instance-1")
	if got != filepath.Join(os.TempDir(), "dctl-command-instance-1.sock") || !strings.HasSuffix(got, ".sock") {
		t.Fatalf("CommandSocketPath(instance-1) = %q", got)
	}
}
