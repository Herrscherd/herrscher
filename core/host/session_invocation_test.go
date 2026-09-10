package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// La fiche d'une session ne disait que le vendeur, alors que le modele et
// l'effort vivent l'un dans le catalogue, l'autre dans l'invocation.
func TestSessionModelAndEffortComeFromTheCatalogThenTheInvocation(t *testing.T) {
	for _, c := range []struct {
		name          string
		sess          state.Session
		model, effort string
	}{
		{
			name:   "le catalogue prime sur l'invocation",
			sess:   state.Session{ModelID: "claude-opus-5", Cmd: "claude --model claude-opus-4-8 --effort low"},
			model:  "claude-opus-5",
			effort: "low",
		},
		{
			name:   "sans catalogue, l'invocation dit le modele",
			sess:   state.Session{Cmd: "claude --model claude-opus-4-8 --effort medium"},
			model:  "claude-opus-4-8",
			effort: "medium",
		},
		{
			name:   "la forme collee est lue aussi",
			sess:   state.Session{Cmd: "codex --model=gpt-5.6 --effort=high"},
			model:  "gpt-5.6",
			effort: "high",
		},
		{
			name: "un backend sans ces drapeaux ne rend rien",
			sess: state.Session{Cmd: "cursor-agent"},
		},
		{
			name: "un drapeau sans valeur ne rend rien",
			sess: state.Session{Cmd: "claude --model"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := sessionModel(c.sess); got != c.model {
				t.Errorf("modele = %q, attendu %q", got, c.model)
			}
			if got := sessionEffort(c.sess); got != c.effort {
				t.Errorf("effort = %q, attendu %q", got, c.effort)
			}
		})
	}
}
