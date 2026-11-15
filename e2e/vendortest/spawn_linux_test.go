//go:build linux

package vendortest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
