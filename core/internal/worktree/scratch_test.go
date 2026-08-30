package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScratchIsIdempotentAndRemovable(t *testing.T) {
	w := &Worktreer{instanceID: "inst"}
	root := t.TempDir()

	p, err := w.Scratch(root, "demo")
	if err != nil {
		t.Fatalf("scratch: %v", err)
	}
	if want := filepath.Join(root, ".herrscher-scratch", "inst", "demo"); p != want {
		t.Fatalf("path = %q, want %q", p, want)
	}
	if err := os.WriteFile(filepath.Join(p, "SOUL.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	again, err := w.Scratch(root, "demo")
	if err != nil || again != p {
		t.Fatalf("a session that comes back must find the directory it left: %q %v", again, err)
	}
	if _, err := os.Stat(filepath.Join(p, "SOUL.md")); err != nil {
		t.Fatalf("provisioning was wiped by a second Scratch: %v", err)
	}

	if err := w.RemoveScratch(root, "demo"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("directory should be gone, stat err = %v", err)
	}
	if err := w.RemoveScratch(root, "demo"); err != nil {
		t.Fatalf("removing what is already gone is not a failure: %v", err)
	}
}

func TestScratchRefusesNamesThatWouldEscapeTheRoot(t *testing.T) {
	w := &Worktreer{instanceID: "inst"}
	root := t.TempDir()
	for _, name := range []string{"..", "../..", "a/b", ""} {
		if _, err := w.Scratch(root, name); err == nil {
			t.Fatalf("Scratch(%q) should be refused", name)
		}
		if err := w.RemoveScratch(root, name); err == nil {
			t.Fatalf("RemoveScratch(%q) should be refused", name)
		}
	}
	if _, err := w.Scratch("", "demo"); err == nil {
		t.Fatal("Scratch with no root should be refused")
	}
}
