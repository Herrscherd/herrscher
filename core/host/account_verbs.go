package host

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Herrscherd/herrscher/core/cli"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	paramProfile     = "profile"
	paramFresh       = "fresh"
	freshMarker      = "fresh"
	nativeChoice     = contracts.NativeProfile
	noProfileChoice  = ""
	listAccountsHelp = "list the account profiles of a session's vendor, marking the default and the session's choice"
)

var errConversationAccount = errors.New("the session has a conversation under another account: rerun with fresh to start a new conversation, or cancel")

func sessionAccountsVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsSession, "accounts").
		Help(listAccountsHelp).
		Param(paramName, "session name", true).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(listSessionAccounts(store, input.Get(paramName)))
		})
}

func sessionSetAccountVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsSession, "set-account").
		Help("set a session's account profile: a profile name, native for the machine's own login, or empty for the vendor default; a session with a conversation under another account needs fresh").
		Param(paramName, "session name", true).
		Param(paramProfile, "profile name, native, or empty for the vendor default", false).
		Param(paramFresh, "fresh to start a new conversation under the new account", false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(setSessionAccount(store, input.Get(paramName), input.Get(paramProfile), input.Get(paramFresh) == freshMarker))
		})
}

func findSession(store *state.State, name string) (state.Session, bool) {
	sessions := store.SnapshotSessions()
	for index := range sessions {
		if sessions[index].Name == name {
			return sessions[index], true
		}
	}
	return state.Session{}, false
}

func listSessionAccounts(store *state.State, name string) (string, error) {
	sess, ok := findSession(store, name)
	if !ok {
		return "", &NotFoundError{Kind: kindSession, Name: name}
	}
	root, err := accountRoot(BackendRequest{})
	if err != nil {
		return "", err
	}
	profiles, err := accounts.Store{Root: root}.List()
	if err != nil {
		return "", err
	}
	lines := []string{sess.Vendor + ": native"}
	for _, profile := range profiles {
		if profile.Vendor != sess.Vendor {
			continue
		}
		lines = append(lines, profileLine(profile, sess.Account))
	}
	return strings.Join(lines, "\n"), nil
}

func profileLine(profile accounts.Profile, chosen string) string {
	line := profile.Name
	if profile.Default {
		line += " (default)"
	}
	if chosen == profile.Name {
		line += " (this session)"
	}
	return line
}

func setSessionAccount(store *state.State, name, profile string, fresh bool) (string, error) {
	sess, ok := findSession(store, name)
	if !ok {
		return "", &NotFoundError{Kind: kindSession, Name: name}
	}
	stored := strings.TrimSpace(profile)
	if err := validateProfileChoice(sess.Vendor, stored); err != nil {
		return "", err
	}
	if accountChangeBlocked(sess, stored, fresh) {
		return "", fmt.Errorf("%s: %w", name, errConversationAccount)
	}
	found, err := store.SetAccount(name, stored, fresh)
	if err != nil {
		return "", err
	}
	if !found {
		return "", &NotFoundError{Kind: kindSession, Name: name}
	}
	return accountChangeNotice(name, stored, fresh), nil
}

func accountChangeNotice(name, profile string, fresh bool) string {
	account := accountLabel(profile)
	if fresh {
		return fmt.Sprintf("compte de %s réglé sur %s : au prochain lancement, la conversation précédente n'est plus reprise et une nouvelle s'ouvre sous ce compte ; le bridge en cours garde l'ancien compte jusqu'au prochain lancement", name, account)
	}
	return fmt.Sprintf("compte de %s réglé sur %s : effectif au prochain lancement de la session ; le bridge en cours garde l'ancien compte", name, account)
}

func accountLabel(profile string) string {
	switch profile {
	case noProfileChoice:
		return "le défaut du vendeur"
	case nativeChoice:
		return "le compte natif"
	}
	return profile
}

func accountChangeBlocked(sess state.Session, profile string, fresh bool) bool {
	return !fresh && sess.ResumeToken != "" && sess.Account != profile
}

func validateProfileChoice(vendor, profile string) error {
	if profile == noProfileChoice || profile == nativeChoice {
		return nil
	}
	root, err := accountRoot(BackendRequest{})
	if err != nil {
		return err
	}
	_, _, resolveErr := accounts.Store{Root: root}.Resolve(profile, vendor)
	return resolveErr
}
