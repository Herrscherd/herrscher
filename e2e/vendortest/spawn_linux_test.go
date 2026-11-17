//go:build linux

package vendortest

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func descendantToken(t *testing.T, child *exec.Cmd) string {
	t.Helper()
	for _, entry := range child.Env {
		if value, found := strings.CutPrefix(entry, DescendantMarker+"="); found {
			return value
		}
	}
	t.Fatalf("the descendant carries no %s", DescendantMarker)
	return ""
}

func TestSpawnGivesEachDescendantItsOwnToken(t *testing.T) {
	files := scriptFiles(t, "text Voici\n")
	r := &replay{files: files}
	t.Cleanup(func() {
		if stopErr := r.stopDescendants(); stopErr != nil {
			t.Error(stopErr)
		}
	})
	for range 2 {
		if err := r.spawn("descendant.pid"); err != nil {
			t.Fatal(err)
		}
	}
	first := descendantToken(t, r.descendants[0])
	last := descendantToken(t, r.descendants[1])
	if first == last {
		t.Fatalf("two descendants of one run share the token %q: a descendant of another scenario would pass as ours", first)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(files.Log), "descendant.pid"))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[0] != strconv.Itoa(r.descendants[1].Process.Pid) || fields[1] != last {
		t.Fatalf("the pid file %q does not name the last descendant and its token", data)
	}
}

func TestOneShotRealPromptConsumesATurn(t *testing.T) {
	files := scriptFiles(t, "spawn descendant.pid\ntext Voici\n---\ntext second\n")
	args := []string{"-p", "say ping please"}
	r, err := openReplay(files, args)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if stopErr := r.stopDescendants(); stopErr != nil {
			t.Error(stopErr)
		}
	})
	var out bytes.Buffer
	if callErr := r.oneShot(&out, args); callErr != nil {
		t.Fatal(callErr)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(files.Log), "descendant.pid")); statErr != nil {
		t.Fatalf("a real -p turn spawned no descendant: %v", statErr)
	}
	var reply event
	if decodeErr := json.Unmarshal(out.Bytes(), &reply); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if reply.Result != "Voici" {
		t.Fatalf("got reply %q, want the text of turn 0", reply.Result)
	}
	turn, err := r.nextTurn()
	if err != nil {
		t.Fatal(err)
	}
	if len(turn.steps) != 1 || turn.steps[0].text != "second" {
		t.Fatalf("the real -p prompt did not consume turn 0: got %+v", turn)
	}
}
