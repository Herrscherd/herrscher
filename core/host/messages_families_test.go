package host

import (
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

type frozenMessage struct {
	key    string
	params map[string]any
	fr     string
	en     string
}

func TestHostMessageFamiliesAreFrozen(t *testing.T) {
	families := map[string][]frozenMessage{
		"fork": {
			{"fork.failed", map[string]any{"Name": "alpha", "Detail": "boom"}, `bifurcation de "alpha" : boom`, `fork "alpha": boom`},
			{"fork.checkpoint_not_applied", map[string]any{"Name": "alpha", "Position": 3, "Detail": "boom"}, `bifurcation de "alpha" : point de contrôle 3 non appliqué : boom`, `fork "alpha": checkpoint 3 not applied: boom`},
			{"fork.not_resumed", map[string]any{"Name": "alpha", "Detail": "boom"}, `bifurcation de "alpha" : conversation non reprise : boom`, `fork "alpha": conversation not resumed: boom`},
		},
		"checkpoint": {
			{"checkpoint.rewind_resume", map[string]any{"Name": "alpha", "Detail": "boom"}, "retour arrière de alpha : conversation non reprise : boom", "rewind alpha: conversation not resumed: boom"},
			{"checkpoint.index", map[string]any{"Detail": "boom", "Got": "x"}, `boom, reçu "x"`, `boom, got "x"`},
		},
		"gateway": {
			{"gateway.model", map[string]any{"Model": "m1", "Detail": "boom"}, `le modèle "m1" exige la passerelle Neublox : boom`, `model "m1" needs the Neublox gateway: boom`},
			{"gateway.base_url", map[string]any{"Detail": "boom"}, "URL de base de la passerelle : boom", "gateway base URL: boom"},
		},
		"coordination": {
			{"coordination.delegate_refused", nil, "delegate refusé", "delegate refused"},
			{"coordination.report_refused", nil, "report refusé", "report refused"},
			{"coordination.report_delivered", map[string]any{"Parent": "lead"}, "rapport livré à lead", "report delivered to lead"},
			{"coordination.merge_refused", map[string]any{"Detail": "boom"}, "fusion refusée : boom", "merge refused: boom"},
			{"coordination.refused", map[string]any{"Detail": "boom"}, "coordination refusée : boom", "coordination refused: boom"},
		},
		"handoff": {
			{"handoff.not_delivered", map[string]any{"Name": "beta", "Detail": "boom"}, `la session "beta" a été créée mais le brief ne l'a pas atteinte : boom`, `session "beta" was created but the brief did not reach it: boom`},
			{"handoff.brief_from", map[string]any{"Source": "alpha", "Detail": "boom"}, `le brief de "alpha" : boom`, `the brief from "alpha": boom`},
		},
		"turn": {
			{"turn.budget_refused", map[string]any{"Reason": "daily"}, "tour refusé, plafond de budget atteint (daily) ; augmentez le plafond puis renvoyez le message", "turn refused, budget ceiling reached (daily); raise the ceiling, then resend the message"},
			{"turn.token_ceiling", map[string]any{"Spent": "80", "Margin": "20"}, "tour interrompu, plafond de tokens atteint (80 tokens sur ce tour, marge 20)", "turn interrupted, token ceiling reached (80 tokens this turn, margin 20)"},
		},
		"models": {
			{"models.cache_read", map[string]any{"Detail": "boom"}, "lecture du cache des modèles impossible : boom", "read model cache: boom"},
			{"models.cache_lock", map[string]any{"Detail": "boom"}, "verrouillage du cache des modèles impossible : boom", "lock model cache: boom"},
		},
		"remote": {
			{"remote.no_remote", map[string]any{"Category": "gpu", "Detail": "boom"}, "résolveur : aucun distant gpu n'a annoncé son adresse : boom", "resolver: no remote gpu announced: boom"},
			{"remote.stage_agent", map[string]any{"Name": "scripter", "Detail": "boom"}, `préparation de l'agent "scripter" impossible : boom`, `stage agent "scripter": boom`},
		},
		"provision": {
			{"provision.smoke", map[string]any{"Host": "h", "Detail": "boom"}, "le binaire déposé sur h échoue à son test de fumée : boom", "the binary put on h fails its smoke test: boom"},
		},
		"hosts": {
			{"hosts.clone", map[string]any{"Project": "p", "Host": "h", "Detail": "boom"}, "impossible de cloner p sur h : boom", "cannot clone p onto h: boom"},
		},
		"daemon": {
			{"daemon.events_socket", map[string]any{"Path": "/p", "Detail": "boom"}, "socket d'événements /p : boom", "events socket /p: boom"},
		},
		"seed": {
			{"seed.consolidate", map[string]any{"Name": "alpha", "Elapsed": "1s", "Detail": "boom"}, `herrscher : session "alpha" : consolidation après 1s : boom`, `herrscher: session "alpha": consolidate after 1s: boom`},
		},
		"serve": {
			{"serve.config", map[string]any{"Detail": "boom"}, "chargement de la configuration impossible : boom", "load config: boom"},
			{"serve.key_proxy", map[string]any{"Detail": "boom"}, "proxy de clés : boom", "key proxy: boom"},
		},
		"session": {
			{"session.percent", map[string]any{"Raw": "x", "Detail": "boom"}, `pourcentage "x" : boom`, `percent "x": boom`},
			{"session.read", map[string]any{"Detail": "boom"}, "lecture des sessions impossible : boom", "read sessions: boom"},
		},
	}
	for family, messages := range families {
		t.Run(family, func(t *testing.T) {
			for _, msg := range messages {
				if got := newHostMessages().text(i18n.French, msg.key, msg.params); got != msg.fr {
					t.Errorf("%s in fr = %q, want %q", msg.key, got, msg.fr)
				}
				if got := newHostMessages().text(i18n.English, msg.key, msg.params); got != msg.en {
					t.Errorf("%s in en = %q, want %q", msg.key, got, msg.en)
				}
			}
		})
	}
}

func TestSessionlessOperatorFrenchReachesTheWrappedMessage(t *testing.T) {
	live := newOperatorLanguage(i18n.French)
	err := busyRewindError("alpha")
	got := localizeHostError(commandErrorLanguage(nil, live.Get(), contracts.Input{}), err).Error()
	want := `la session "alpha" a un tour en cours : interrompez-le (échap) avant de revenir en arrière`
	if got != want {
		t.Fatalf("sessionless fr operator got %q, want %q", got, want)
	}
	wrapped := fmt.Errorf("commande refusée: %w", busyRewindError("alpha"))
	gotWrapped := localizeHostError(commandErrorLanguage(nil, live.Get(), contracts.Input{}), wrapped).Error()
	wantWrapped := "commande refusée: " + want
	if gotWrapped != wantWrapped {
		t.Fatalf("fmt-wrapped sessionless fr operator got %q, want %q", gotWrapped, wantWrapped)
	}
}
