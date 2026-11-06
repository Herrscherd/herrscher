package host

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Herrscherd/herrscher/core/cli"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	paramProfile    = "profile"
	paramFresh      = "fresh"
	freshMarker     = "fresh"
	nativeChoice    = contracts.NativeProfile
	noProfileChoice = ""
)

var errConversationAccount = errors.New("the session has a conversation under another account: rerun with fresh to start a new conversation, or cancel")

func sessionAccountsVerb(lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New(nsSession, "accounts").
		Help(clitext.CmdHelpSessionAccounts).
		Param(paramName, clitext.CmdParamSessionAccountsName, true).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(listSessionAccounts(lang(), store, input.Get(paramName)))
		})
}

func sessionSetAccountVerb(lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New(nsSession, "set-account").
		Help(clitext.CmdHelpSessionSetAccount).
		Param(paramName, clitext.CmdParamSessionSetAccountName, true).
		Param(paramProfile, clitext.CmdParamSessionSetAccountProfile, false).
		Param(paramFresh, clitext.CmdParamSessionSetAccountFresh, false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(setSessionAccount(lang(), store, input.Get(paramName), input.Get(paramProfile), input.Get(paramFresh) == freshMarker))
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

func listSessionAccounts(lang i18n.Language, store *state.State, name string) (string, error) {
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
	text := replyText(lang)
	lines := []string{sess.Vendor + ": " + text.Text(clitext.AccountNativeLabel, nil)}
	for _, profile := range profiles {
		if profile.Vendor != sess.Vendor {
			continue
		}
		lines = append(lines, profileLine(text, profile, sess.Account))
	}
	return strings.Join(lines, "\n"), nil
}

func profileLine(text *clitext.Translator, profile accounts.Profile, chosen string) string {
	line := profile.Name
	if profile.Default {
		line += " " + text.Text(clitext.AccountDefaultMarker, nil)
	}
	if chosen == profile.Name {
		line += " " + text.Text(clitext.SessionAccountThis, nil)
	}
	return line
}

func setSessionAccount(lang i18n.Language, store *state.State, name, profile string, fresh bool) (string, error) {
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
	return accountChangeNotice(lang, name, stored, fresh), nil
}

func accountChangeNotice(lang i18n.Language, name, profile string, fresh bool) string {
	text := replyText(lang)
	var key clitext.Key = clitext.SessionAccountNotice
	if fresh {
		key = clitext.SessionAccountNoticeFresh
	}
	return text.Text(key, clitext.Params{clitext.ParamName: name, clitext.ParamAccount: accountLabel(text, profile)})
}

func accountLabel(text *clitext.Translator, profile string) string {
	switch profile {
	case noProfileChoice:
		return text.Text(clitext.SessionAccountVendorDefault, nil)
	case nativeChoice:
		return text.Text(clitext.SessionAccountNative, nil)
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
