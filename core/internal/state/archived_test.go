package state

import (
	"path/filepath"
	"testing"
)

func TestSetArchivedPersistsOnChange(t *testing.T) {
	dir := t.TempDir()
	st := NewState(filepath.Join(dir, "state.json"))
	if err := st.AddSession(Session{Name: "s", ChannelID: "c", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetArchived("s", true); err != nil {
		t.Fatal(err)
	}
	got, _ := st.FindSession("s")
	if !got.Archived {
		t.Fatalf("archived not set")
	}
	// unknown name is a no-op, not an error
	if err := st.SetArchived("nope", true); err != nil {
		t.Fatalf("unknown should be nil, got %v", err)
	}
}
