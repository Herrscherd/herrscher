package tui

import (
	"reflect"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// /rename nomme la session ouverte : l'operateur donne le nouveau nom, la
// fenetre sait deja de laquelle il parle.
func TestRenameNamesTheActiveSession(t *testing.T) {
	be := &fakeBackend{}
	be.sessions = []contracts.SessionInfo{{Name: "avant", ChannelID: "c1"}}
	m := newModel(be)
	m.ensureTab("c1")
	m.active = "c1"
	m.syncTabs()

	m.input.SetValue("/rename ma grande refonte")
	runCmd(m.handleEnter())

	want := []string{"session", "rename", "--name", "avant", "--to", "ma grande refonte"}
	if len(be.dispatched) != 1 || !reflect.DeepEqual(be.dispatched[0], want) {
		t.Fatalf("commande envoyee = %v, attendu %v", be.dispatched, want)
	}
}

// Un /rename sans nom ne doit rien envoyer : renommer une session en rien est
// une erreur de frappe, pas une intention.
func TestRenameWithoutANameSaysSoAndSendsNothing(t *testing.T) {
	be := &fakeBackend{}
	be.sessions = []contracts.SessionInfo{{Name: "avant", ChannelID: "c1"}}
	m := newModel(be)
	m.ensureTab("c1")
	m.active = "c1"
	m.syncTabs()

	m.input.SetValue("/rename")
	runCmd(m.handleEnter())

	if len(be.dispatched) != 0 {
		t.Fatalf("rien ne doit partir: %v", be.dispatched)
	}
	if !strings.Contains(m.flash, "/rename") {
		t.Fatalf("l'operateur doit lire ce qui manque: %q", m.flash)
	}
}

func TestRenameTargetReadsBothSpellings(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"nouveau"}, "nouveau"},
		{[]string{"deux", "mots"}, "deux mots"},
		{[]string{"--to", "nouveau"}, "nouveau"},
		{[]string{"--to=nouveau"}, "nouveau"},
		{[]string{"--to"}, ""},
		{nil, ""},
	} {
		if got := renameTarget(c.in); got != c.want {
			t.Errorf("renameTarget(%v) = %q, attendu %q", c.in, got, c.want)
		}
	}
}
