package clitext

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher-contracts/i18n/i18ntest"
)

func sources(t *testing.T) fs.FS {
	t.Helper()
	fsys, err := fs.Sub(locales, "locales")
	if err != nil {
		t.Fatal(err)
	}
	return fsys
}

func declaredByName() map[string][]string {
	out := make(map[string][]string, len(declaredKeys()))
	for key, fields := range declaredKeys() {
		out[string(key)] = fields
	}
	return out
}

func TestEveryDeclaredKeyHasItsTextInEveryLanguage(t *testing.T) {
	i18ntest.CheckDeclared(t, declaredByName(), sources(t))
}

func TestEveryKeyFileIsDeclaredOnce(t *testing.T) {
	groups := []map[Key][]string{usageKeys(), cliKeys(), serveKeys(), bridgeKeys(), sessionKeys(), pluginKeys(), serviceKeys(), accountKeys()}
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total != len(declaredKeys()) {
		t.Fatalf("keys declared %d times for %d distinct keys: a key sits in two groups", total, len(declaredKeys()))
	}
}

func TestTextRefusesAnUndeclaredKey(t *testing.T) {
	_, err := Text(i18n.English, Key("cli.nowhere"), nil)
	if !errors.Is(err, ErrUndeclaredKey) {
		t.Fatalf("Text on an undeclared key = %v, want ErrUndeclaredKey", err)
	}
}

func TestTextRefusesParamsThatDoNotMatchTheKey(t *testing.T) {
	_, err := Text(i18n.English, UsageTagline, Params{"Unused": "x"})
	if !errors.Is(err, ErrParamsMismatch) {
		t.Fatalf("Text with an undeclared param = %v, want ErrParamsMismatch", err)
	}
}

func TestTranslatorKeepsTheFirstFailure(t *testing.T) {
	tr := For(i18n.French)
	got := tr.Text(Key("cli.nowhere"), nil)
	_ = tr.Text(Key("cli.elsewhere"), nil)
	if got != "cli.nowhere" {
		t.Fatalf("Text after a failure = %q, want the key itself", got)
	}
	if !errors.Is(tr.Err(), ErrUndeclaredKey) || !strings.Contains(tr.Err().Error(), "cli.nowhere") {
		t.Fatalf("Err = %v, want the first failure, on cli.nowhere", tr.Err())
	}
}

func TestRenderedMessagesInEachLanguage(t *testing.T) {
	cases := []struct {
		name   string
		key    Key
		params Params
		en     string
		fr     string
	}{
		{"attach no session", AttachNoSession, nil, "herrscher: no session is bound to this tab any more", "herrscher : aucune session n'est plus liée à cet onglet"},
		{"error line", ErrorLine, Params{"Message": "boom"}, "herrscher: boom", "herrscher : boom"},
		{"unknown command", CommandUnknown, Params{"Name": `"x"`}, `unknown command "x"`, `commande inconnue "x"`},
		{"env set failed", EnvSetFailed, Params{"Key": "A", "Err": "bad"}, "set A: bad", "définition de A : bad"},
		{"attach not available", AttachNotAvailable, Params{"Verb": `"agent"`}, `command "agent" is not available from the terminal (allowed: session, agent)`, `commande "agent" indisponible depuis le terminal (autorisées : session, agent)`},
		{"serve state flag", ServeFlagState, nil, "path to the daemon state file", "chemin du fichier d'état du démon"},
		{"service restarted", ServiceRestarted, nil, "herrscher service: restarted", "herrscher service : redémarré"},
		{"service unknown subcommand", ServiceUnknownSubcommand, Params{"Name": `"x"`}, `herrscher service: unknown subcommand "x" (want install|uninstall|status|restart|update)`, `herrscher service : sous-commande inconnue "x" (valeurs possibles : install|uninstall|status|restart|update)`},
		{"service note running", ServiceNoteRunning, Params{"EnvFile": "/e"}, "herrscher service: running. Secrets live in /e; edit it and restart to change them.", "herrscher service : en cours d'exécution. Les secrets sont dans /e ; modifiez-le puis redémarrez pour les changer."},
		{"serve gateway retrying", ServeGatewayRetrying, Params{"Failure": "relay: offline", "Wait": "2s"}, "herrscher: gateway not up yet: relay: offline; retrying in 2s", "herrscher : passerelle pas encore prête : relay: offline ; nouvel essai dans 2s"},
		{"serve no gateway", ServeNoGateway, nil, "no gateway built", "aucune passerelle construite"},
		{"bridge model not routable", BridgeModelNotRoutable, Params{"Model": `"gw-claude"`}, `model "gw-claude" cannot be routed through the remote backend resolver (HERRSCHER_REMOTE=backend): the proxy cannot carry the model's spawn environment`, `le modèle "gw-claude" ne peut pas passer par le résolveur de backend distant (HERRSCHER_REMOTE=backend) : le proxy ne transporte pas l'environnement de lancement du modèle`},
		{"bridge env stdin failed", BridgeEnvStdinFailed, Params{"Key": "A", "Err": "bad"}, "apply A from stdin: bad", "application de A depuis l'entrée standard : bad"},
		{"bridge env stdin flag", BridgeFlagEnvStdin, nil, "read KEY=VALUE lines from stdin until a blank line and put them in this process's environment (how a remote bridge gets its gateway credentials)", "lit des lignes KEY=VALUE sur l'entrée standard jusqu'à une ligne vide et les place dans l'environnement de ce processus (c'est ainsi qu'un bridge distant reçoit ses identifiants de passerelle)"},
		{"bridge vendor flag", BridgeFlagVendor, nil, "backend vendor: claude | codex | cursor (empty = first registered / HERRSCHER_BACKEND)", "fournisseur du backend : claude | codex | cursor (vide = premier enregistré, ou HERRSCHER_BACKEND)"},
		{"serve session failed", ServeSessionFailed, Params{"Name": "demo", "Err": "boom"}, "session demo: boom", "session demo : boom"},
		{"account default", AccountDefault, Params{"Vendor": "claude", "Name": "perso"}, "claude default: perso", "claude par défaut : perso"},
		{"account resuming one", AccountResuming, Params{"Count": 1, "Vendor": "claude", "Sessions": "alpha"}, "attention: 1 session claude will resume under this default: alpha. To keep it on another account, use herrscher session set-account <session> <profile> (or fresh for a new conversation).", "attention : 1 session claude va reprendre sous ce défaut : alpha. Pour la garder sur un autre compte, utilisez herrscher session set-account <session> <profil> (ou fresh pour une nouvelle conversation)."},
		{"account resuming many", AccountResuming, Params{"Count": 2, "Vendor": "claude", "Sessions": "alpha, beta"}, "attention: 2 sessions claude will resume under this default: alpha, beta. To keep them on another account, use herrscher session set-account <session> <profile> (or fresh for a new conversation).", "attention : 2 sessions claude vont reprendre sous ce défaut : alpha, beta. Pour les garder sur un autre compte, utilisez herrscher session set-account <session> <profil> (ou fresh pour une nouvelle conversation)."},
		{"account in use", AccountInUse, Params{"Vendor": "claude", "Name": "perso", "Sessions": "alpha, beta"}, "claude/perso used by session(s) alpha, beta, move them with herrscher session set-account first", "claude/perso utilisé par la ou les sessions alpha, beta, déplacez-les d'abord avec herrscher session set-account"},
		{"env read failed", EnvReadFailed, Params{"Err": "bad"}, "read env file: bad", "lecture du fichier d'environnement : bad"},
		{"announce key invalid", TransportAnnounceKeyInvalid, Params{"Err": "short"}, "HERRSCHER_ANNOUNCE_KEY: short", "HERRSCHER_ANNOUNCE_KEY : short"},
		{"skill duplicate", SkillDuplicate, Params{"Name": "demo", "Kind": "bundle", "Owner": "core"}, "herrscher: skill demo from plugin bundle was left out: plugin core already ships one by that name", "herrscher : la compétence demo du plugin bundle a été écartée : le plugin core en livre déjà une de ce nom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := Text(i18n.English, tc.key, tc.params); err != nil || got != tc.en {
				t.Fatalf("en = %q, %v; want %q", got, err, tc.en)
			}
			if got, err := Text(i18n.French, tc.key, tc.params); err != nil || got != tc.fr {
				t.Fatalf("fr = %q, %v; want %q", got, err, tc.fr)
			}
		})
	}
}
