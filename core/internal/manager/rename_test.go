package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// Renommer une session doit deplacer tout ce qui porte son nom : la ligne
// persistee garde son identite, le transcript et le journal la suivent, et le
// pont repart sous le nouveau nom pour ne pas continuer d'ecrire dans les
// fichiers qui viennent de bouger.
func TestSessionRenameCarriesTheTranscriptAndTheJournal(t *testing.T) {
	h, _, sup, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "avant")); err != nil {
		t.Fatal(err)
	}
	before, _ := st.FindSession("avant")
	old := state.TranscriptPath(h.PartDir(), "avant")
	if err := state.AppendTranscript(old, state.TranscriptEntry{Ts: "t", Role: "user", Text: "salut"}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.AppendParticipant(state.ParticipantsPath(h.PartDir(), "avant"), "u1"); err != nil {
		t.Fatal(err)
	}
	sup.started, sup.stopped = nil, nil

	out, err := h.sessionRenameRun(context.Background(), args("name", "avant", "to", "Apres Le Renommage"))
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, still := st.FindSession("avant"); still {
		t.Fatalf("l'ancien nom doit disparaitre: %s", out)
	}
	after, ok := st.FindSession("apres-le-renommage")
	if !ok {
		t.Fatalf("le nouveau nom doit exister: %s", out)
	}
	if after.ID != before.ID || after.Incarnation != before.Incarnation {
		t.Fatalf("un renommage garde l'identite: %q/%q puis %q/%q", before.ID, before.Incarnation, after.ID, after.Incarnation)
	}
	if got := state.ReadTranscript(state.TranscriptPath(h.PartDir(), "apres-le-renommage"), 0); len(got) != 1 {
		t.Fatalf("le transcript doit suivre, %d entrees trouvees", len(got))
	}
	if got := state.ReadTranscript(old, 0); got != nil {
		t.Fatalf("rien ne doit rester sous l'ancien nom: %v", got)
	}
	if got := state.ReadParticipants(state.ParticipantsPath(h.PartDir(), "apres-le-renommage")); len(got) != 1 {
		t.Fatalf("le journal des participants doit suivre: %v", got)
	}
	if len(sup.stopped) == 0 || sup.stopped[0] != "avant" {
		t.Fatalf("le pont doit etre arrete sous l'ancien nom: %v", sup.stopped)
	}
	if len(sup.started) == 0 || sup.started[0] != "apres-le-renommage" {
		t.Fatalf("le pont doit repartir sous le nouveau nom: %v", sup.started)
	}
}

func TestSessionRenameRefusesTheImpossible(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})
	for _, n := range []string{"un", "deux"} {
		if _, err := h.sessionCreateRun(context.Background(), args("name", n)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.sessionRenameRun(context.Background(), args("name", "fantome", "to", "trois")); err == nil {
		t.Errorf("renommer une session inconnue doit echouer")
	}
	if _, err := h.sessionRenameRun(context.Background(), args("name", "un", "to", "deux")); err == nil {
		t.Errorf("ecraser une session existante doit echouer")
	}
	if _, err := h.sessionRenameRun(context.Background(), args("name", "un", "to", "../")); err == nil {
		t.Errorf("un nom qui ne laisse aucun slug doit echouer")
	}
	if out, err := h.sessionRenameRun(context.Background(), args("name", "un", "to", "un")); err != nil || !strings.Contains(out, "déjà") {
		t.Errorf("renommer vers le meme nom ne fait rien et le dit: %q %v", out, err)
	}
	if _, ok := st.FindSession("un"); !ok {
		t.Fatalf("un renommage refuse ne doit rien casser")
	}
}

// Un horaire qui vise la session par son nom doit suivre le renommage. Sans
// cela sa fenetre est sautee en silence a chaque tick : le planificateur cherche
// un nom que plus personne ne porte.
func TestSessionRenameCarriesTheSchedulesThatTargetIt(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "avant")); err != nil {
		t.Fatal(err)
	}
	if err := st.PutSchedule(schedule.Schedule{Name: "veille", Session: "avant", Task: "regarde", Every: "1h", CreatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutSchedule(schedule.Schedule{Name: "autre", Session: "voisine", Task: "rien", Every: "1h", CreatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionRenameRun(context.Background(), args("name", "avant", "to", "apres")); err != nil {
		t.Fatalf("rename: %v", err)
	}

	for _, sc := range st.SnapshotSchedules() {
		switch sc.Name {
		case "veille":
			if sc.Session != "apres" {
				t.Fatalf("l'horaire vise encore %q", sc.Session)
			}
		case "autre":
			if sc.Session != "voisine" {
				t.Fatalf("un horaire etranger a ete reecrit: %q", sc.Session)
			}
		}
	}
}

// Une session possedee par un horaire a cible agent porte un nom derive de
// l'horaire, que le renommage ne peut pas suivre. L'operateur doit etre averti
// plutot que refuse : le prochain tick rouvrira une session sous l'ancien nom.
func TestSessionRenameWarnsWhenAScheduleOwnsTheName(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.SetHome(state.HomeRef{ID: "cat1", Type: "category"})

	if _, err := h.sessionCreateRun(context.Background(), args("name", "schedule-veille")); err != nil {
		t.Fatal(err)
	}
	if err := st.PutSchedule(schedule.Schedule{Name: "veille", Agent: "scout", Task: "regarde", Every: "1h", CreatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	out, err := h.sessionRenameRun(context.Background(), args("name", "schedule-veille", "to", "veille-manuelle"))
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !strings.Contains(out, "veille") || !strings.Contains(out, "⚠️") {
		t.Fatalf("le renommage doit avertir que l'horaire possede le nom: %q", out)
	}
	if _, ok := st.FindSession("veille-manuelle"); !ok {
		t.Fatal("le renommage doit quand meme avoir lieu")
	}
}
