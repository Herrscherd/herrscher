package host

import (
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
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
			{"remote.unavailable", map[string]any{"Attempts": 3, "Category": "gpu", "Detail": "boom", "Elapsed": "2s"}, "résolveur : ressource distante gpu indisponible après 3 tentative(s) en 2s : boom", "resolver: remote gpu unavailable after 3 attempt(s) in 2s: boom"},
			{"remote.stage_agent", map[string]any{"Name": "scripter", "Detail": "boom"}, `préparation de l'agent "scripter" impossible : boom`, `stage agent "scripter": boom`},
			{"remote.extract_archive", map[string]any{"Path": "/w", "Detail": "boom"}, "décompression de l'archive de l'agent dans /w impossible : boom", "cannot unpack the agent archive into /w: boom"},
			{"remote.extract_member", map[string]any{"Member": "a.txt", "Detail": "boom"}, "extraction de a.txt impossible : boom", "cannot extract a.txt: boom"},
			{"remote.unsafe_member", map[string]any{"Member": "../x", "Root": "/w"}, "élément d'archive refusé : ../x sortirait de /w", "refusing archive member ../x: it lands outside /w"},
		},
		"approval": {
			{"approval.prompt", map[string]any{"Request": "Bash(ls)"}, "autoriser Bash(ls) ?", "allow Bash(ls)?"},
			{"approval.hint", map[string]any{"Command": "approve allow|deny id1", "Help": "approve allow --help"}, " (répondez avec `approve allow|deny id1`, voir `approve allow --help`)", " (answer with `approve allow|deny id1`, see `approve allow --help`)"},
			{"approval.timeout", map[string]any{"Request": "Bash(ls)", "Wait": "5s"}, `herrscher : personne n'a répondu dans 5s, donc Bash(ls) est refusé`, "herrscher: nobody answered within 5s, so Bash(ls) was denied"},
			{"approval.ended", map[string]any{"Request": "Bash(ls)"}, `herrscher : l'attente d'approbation a pris fin, donc Bash(ls) est refusé`, "herrscher: the wait for approval ended, so Bash(ls) was denied"},
			{"approval.denied", map[string]any{"Request": "Bash(ls)"}, "herrscher : Bash(ls) a été refusé par l'opérateur", "herrscher: Bash(ls) was denied by the operator"},
			{"approval.plan_mode", map[string]any{"Request": "Bash(ls)"}, "herrscher : Bash(ls) a été refusé : cette session est en mode plan, qui ne fait que lire ; écris le plan, l'opérateur le valide avant toute écriture", "herrscher: Bash(ls) was refused: this session is in plan mode, which only reads; write the plan, and the operator validates it before anything is written"},
			{"approval.policy_denied", map[string]any{"Request": "Bash(ls)"}, "herrscher : Bash(ls) a été refusé par la politique", "herrscher: Bash(ls) was denied by policy"},
			{"approval.fleet_full", map[string]any{"Request": "Bash(ls)", "Limit": 8}, "herrscher : Bash(ls) a été refusé : 8 appels d'outils de la flotte sont déjà en attente d'un humain, c'est la limite ; chaque réponse libère une place", "herrscher: Bash(ls) was denied: 8 tool calls across the fleet are already waiting for a human, which is the limit; each one answered frees a slot"},
			{"approval.session_full", map[string]any{"Request": "Bash(ls)", "Session": "s1", "Limit": 3}, `herrscher : Bash(ls) a été refusé : la session « s1 » a déjà 3 appels d'outils qui sont en attente d'un humain, c'est la limite ; attends leurs réponses, et demande moins de choses à la fois`, `herrscher: Bash(ls) was denied: session "s1" already has 3 tool calls waiting for a human, which is the limit; wait for those to be answered, and ask about fewer things at once`},
			{"approval.operator_refused", map[string]any{"Reason": "use make clean instead"}, "herrscher : l'opérateur a refusé : use make clean instead", "herrscher: the operator refused: use make clean instead"},
			{"approval.session_not_running", map[string]any{"Request": "Bash(ls)", "Session": "s1"}, "approbations : Bash(ls) a été autorisé une fois : la session « s1 » ne tourne pas, elle ne peut donc pas retenir la réponse", `approvals: Bash(ls) was allowed once: session "s1" is not running, so it cannot remember the answer`},
			{"approval.no_exact_subject", map[string]any{"Request": "Bash(ls)"}, "approbations : Bash(ls) a été autorisé une fois : il n'a pas de sujet exact qu'une règle puisse nommer sans couvrir plus que cette demande", "approvals: Bash(ls) was allowed once: it has no exact subject a rule could name without matching more"},
			{"approval.no_rule_store", map[string]any{"Request": "Bash(ls)"}, "approbations : Bash(ls) a été autorisé une fois : aucun magasin de règles n'est attaché, donc aucune règle n'a été écrite", "approvals: Bash(ls) was allowed once: no rule store is attached, so no rule was written"},
			{"approval.rule_not_saved", map[string]any{"Request": "Bash(ls)", "Rule": "Bash(ls)", "Detail": "boom"}, "approbations : Bash(ls) a été autorisé une fois : la règle « Bash(ls) » n'a pas été enregistrée : boom", `approvals: Bash(ls) was allowed once: the rule "Bash(ls)" was not saved: boom`},
			{"approval.smart_no_model", map[string]any{"Request": "Bash(ls)"}, "approbations : le mode smart n'a pas de modèle pour évaluer Bash(ls), il est donc décidé comme en mode demande", "approvals: smart mode has no model to rate Bash(ls), so it is decided as in ask mode"},
			{"approval.smart_rate_failed", map[string]any{"Request": "Bash(ls)", "Detail": "boom"}, "approbations : le mode smart n'a pas pu évaluer Bash(ls) (boom), il est donc décidé comme en mode demande", "approvals: smart mode could not rate Bash(ls) (boom), so it is decided as in ask mode"},
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
			{"seed.late_transcript", map[string]any{"Name": "alpha"}, `herrscher : session "alpha" : une entrée de transcription reçue après la fermeture du seed a été abandonnée`, `herrscher: session "alpha": a transcript entry arrived after its seed closed and was dropped`},
			{"seed.late_event", map[string]any{"Name": "alpha"}, `herrscher : session "alpha" : un événement reçu après la fermeture du seed a été abandonné`, `herrscher: session "alpha": an event arrived after its seed closed and was dropped`},
			{"seed.late_usage", map[string]any{"Name": "alpha"}, `herrscher : session "alpha" : un relevé d'usage reçu après la fermeture du seed a été abandonné`, `herrscher: session "alpha": a usage record arrived after its seed closed and was dropped`},
			{"seed.late_resume", map[string]any{"Name": "alpha"}, `herrscher : session "alpha" : un jeton de reprise reçu après la fermeture du seed a été abandonné`, `herrscher: session "alpha": a resume token arrived after its seed closed and was dropped`},
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
	err := busyRewindError(newHostMessages(), "alpha")
	got := localizeHostError(newHostMessages(), clitext.NewMessages(), commandErrorLanguage(nil, live.Get(), contracts.Input{}), err).Error()
	want := `la session "alpha" a un tour en cours : interrompez-le (échap) avant de revenir en arrière`
	if got != want {
		t.Fatalf("sessionless fr operator got %q, want %q", got, want)
	}
	wrapped := fmt.Errorf("commande refusée: %w", busyRewindError(newHostMessages(), "alpha"))
	gotWrapped := localizeHostError(newHostMessages(), clitext.NewMessages(), commandErrorLanguage(nil, live.Get(), contracts.Input{}), wrapped).Error()
	wantWrapped := "commande refusée: " + want
	if gotWrapped != wantWrapped {
		t.Fatalf("fmt-wrapped sessionless fr operator got %q, want %q", gotWrapped, wantWrapped)
	}
}
