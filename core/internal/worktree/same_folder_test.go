package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlacementsOfThisMachineThatAreTheSameFolderOnDiskAreTheSame(t *testing.T) {
	folder := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(folder, link); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	cases := []struct {
		name string
		a, b Placement
		same bool
	}{
		{"one folder through a link", Placement{Path: folder}, Placement{Path: link}, true},
		{"one folder with a trailing separator", Placement{Path: folder}, Placement{Path: folder + string(filepath.Separator)}, true},
		{"two folders", Placement{Path: folder}, Placement{Path: other}, false},
		{"one path on two hosts", Placement{Path: folder}, Placement{Host: "build1", Path: folder}, false},
		{"a link on another host is not followed", Placement{Host: "build1", Path: folder}, Placement{Host: "build1", Path: link}, false},
		{"no folder", Placement{}, Placement{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Same(tc.b); got != tc.same {
				t.Fatalf("%+v.Same(%+v) = %v, want %v", tc.a, tc.b, got, tc.same)
			}
		})
	}
}
