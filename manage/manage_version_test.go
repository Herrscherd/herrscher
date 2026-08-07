package manage

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostDir lays out a minimal host module: the managed manifest plus the two go
// files apply saves around a change.
func hostDir(t *testing.T, modules ...string) string {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n\t" + beginMarker + "\n")
	for _, m := range modules {
		b.WriteString(importLine(m) + "\n")
	}
	b.WriteString("\t" + endMarker + "\n)\n")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("plugins.go", b.String())
	write("go.mod", "module host\n\ngo 1.25.0\n")
	return dir
}

func TestManageVersionAddCommands(t *testing.T) {
	cmds := addCommands("mod/a", "v1.2.3")
	if got := strings.Join(cmds[0], " "); got != "go get -- mod/a@v1.2.3" {
		t.Fatalf("first command = %q, want the version passed to go get", got)
	}
	cmds = addCommands("mod/a", "")
	if got := strings.Join(cmds[0], " "); got != "go get -- mod/a" {
		t.Fatalf("first command = %q, want no @ when no version was asked for", got)
	}
	if got := strings.Join(cmds[len(cmds)-1], " "); got != "go install ." {
		t.Fatalf("last command = %q, want the install that replaces the binary", got)
	}
}

func TestManageVersionPinRefusesAbsentModule(t *testing.T) {
	dir := hostDir(t, "mod/a")
	code := PluginCmd(context.Background(), []string{"pin", "--host", dir, "mod/absent"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(dir, pinFile)); !os.IsNotExist(err) {
		t.Fatal("a refused pin still wrote the pin file")
	}
	if err := pinModule(dir, "mod/absent", true); err == nil || !strings.Contains(err.Error(), "mod/absent") {
		t.Fatalf("error = %v, want it to name the module", err)
	}
}

func TestManageVersionPinAndUnpin(t *testing.T) {
	dir := hostDir(t, "mod/a", "mod/b")
	if err := pinModule(dir, "mod/a", true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	pins, err := loadPins(dir)
	if err != nil || !pins["mod/a"] || pins["mod/b"] {
		t.Fatalf("pins = %v, err = %v", pins, err)
	}
	if err := pinModule(dir, "mod/a", false); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if pins, _ := loadPins(dir); pins["mod/a"] {
		t.Fatalf("pins = %v, want mod/a dropped", pins)
	}
}

func TestManageVersionUpdateSkipsPinned(t *testing.T) {
	mods := []string{"mod/a", "mod/b"}
	pins := map[string]bool{"mod/a": true}
	installed := map[string]string{"mod/a": "v1.0.0", "mod/b": "v2.0.0"}

	cmds, skipped := updatePlan(mods, pins, installed)
	for _, c := range cmds {
		if joined := strings.Join(c, " "); joined == "go get -u mod/a" {
			t.Fatalf("a pinned module was bumped: %q", joined)
		}
	}
	if got := strings.Join(cmds[0], " "); got != "go get -u mod/b" {
		t.Fatalf("first command = %q, want the unpinned module bumped", got)
	}
	if len(skipped) != 1 || skipped[0] != "skipped mod/a (pinned v1.0.0)" {
		t.Fatalf("skipped = %v, want a line naming the pin", skipped)
	}
}

func TestManageVersionUpdateAllPinned(t *testing.T) {
	cmds, skipped := updatePlan([]string{"mod/a"}, map[string]bool{"mod/a": true}, nil)
	if cmds != nil {
		t.Fatalf("commands = %v, want none when every module is pinned", cmds)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "mod/a") {
		t.Fatalf("skipped = %v", skipped)
	}
}

func TestManageVersionNoTTYDecidesAutomatically(t *testing.T) {
	in := bufio.NewReader(strings.NewReader(""))
	if _, ok := newDecider(false, false, in).(autoDecider); !ok {
		t.Fatal("a run with no terminal must decide without asking")
	}
	if _, ok := newDecider(true, true, in).(autoDecider); !ok {
		t.Fatal("--yes must decide without asking")
	}
	if _, ok := newDecider(false, true, in).(promptDecider); !ok {
		t.Fatal("an interactive run must ask")
	}
}

func TestManageVersionPluginRows(t *testing.T) {
	rows := pluginRows(
		[]string{"mod/a", "mod/b"},
		map[string]string{"mod/a": "v1.0.0"},
		map[string]bool{"mod/b": true},
		map[string][]string{"mod/a": {"v1.0.0", "v1.1.0"}},
	)
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0].Installed != "v1.0.0" || rows[0].Latest != "v1.1.0" || rows[0].Pinned {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[1].Installed != "?" || rows[1].Latest != "?" || !rows[1].Pinned {
		t.Errorf("row 1 = %+v, want unknowns rendered as ? and the pin kept", rows[1])
	}
}
