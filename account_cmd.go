package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const accountArgCount = 3

var errAccountUsage = errors.New("account usage")

func runAccount(ctx context.Context, messages *clitext.Messages, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return messages.Failure(clitext.ClientLanguage(), err, clitext.AccountResolveHome, clitext.Params{clitext.ParamErr: err.Error()})
	}
	store := accounts.Store{Root: accounts.DefaultRoot(home)}
	if len(args) == 0 {
		return accountUsage(messages, clitext.ClientLanguage())
	}
	switch args[0] {
	case "list":
		return listAccounts(ctx, messages, store, os.Stdout)
	case "add":
		vendor, name, err := accountArgs(messages, args)
		if err != nil {
			return err
		}
		return addAccount(ctx, messages, store, vendor, name, os.Stdin, os.Stdout)
	case "remove":
		vendor, name, err := accountArgs(messages, args)
		if err != nil {
			return err
		}
		return removeAccount(messages, store, vendor, name, os.Stdin, os.Stdout)
	case "default":
		vendor, name, err := accountArgs(messages, args)
		if err != nil {
			return err
		}
		if setErr := store.SetDefault(vendor, name); setErr != nil {
			return setErr
		}
		translator := messages.For(clitext.ClientLanguage())
		if defaultErr := writeText(os.Stdout, translator, clitext.AccountDefault, clitext.Params{clitext.ParamVendor: vendor, clitext.ParamName: string(name)}); defaultErr != nil {
			return defaultErr
		}
		return warnResumingSessions(messages, vendor, os.Stdout)
	}
	return accountUsage(messages, clitext.ClientLanguage())
}

func accountUsage(messages *clitext.Messages, lang i18n.Language) error {
	return messages.Failure(lang, errAccountUsage, clitext.AccountUsage, nil)
}

func accountArgs(messages *clitext.Messages, args []string) (string, contracts.ProfileName, error) {
	if len(args) != accountArgCount {
		return "", "", accountUsage(messages, clitext.ClientLanguage())
	}
	if _, err := contracts.ProfileHomeEnv(args[1]); err != nil {
		return "", "", err
	}
	name, err := contracts.ParseProfileName(args[2])
	if err != nil {
		return "", "", err
	}
	return args[1], name, nil
}

func emitf(output io.Writer, format string, values ...any) error {
	_, err := fmt.Fprintf(output, format, values...)
	return err
}

func writeText(output io.Writer, translator *clitext.Translator, key clitext.Key, params clitext.Params) error {
	text := translator.Text(key, clitext.Printable(params))
	if err := translator.Err(); err != nil {
		return err
	}
	return emitf(output, "%s\n", text)
}

func listAccounts(ctx context.Context, messages *clitext.Messages, store accounts.Store, output io.Writer) error {
	translator := messages.For(clitext.ClientLanguage())
	profiles, err := store.List()
	if err != nil {
		return err
	}
	native := translator.Text(clitext.AccountNativeLabel, nil)
	for _, vendor := range []string{contracts.VendorClaude, contracts.VendorCodex} {
		if err := emitf(output, "%-8s %-10s %s\n", vendor, native, loginState(ctx, translator, vendor, nil)); err != nil {
			return err
		}
	}
	for _, profile := range profiles {
		env, err := contracts.ProfileEnv(profile.Vendor, profile.Dir)
		if err != nil {
			continue
		}
		marker := ""
		if profile.Default {
			marker = " " + translator.Text(clitext.AccountDefaultMarker, nil)
		}
		if err := emitf(output, "%-8s %-10s %s%s\n", termpaint.Escape(profile.Vendor).String(), termpaint.Escape(profile.Name).String(), loginState(ctx, translator, profile.Vendor, env), marker); err != nil {
			return err
		}
	}
	return translator.Err()
}

func loginState(ctx context.Context, translator *clitext.Translator, vendor string, env map[string]string) string {
	connected, err := accounts.Status(ctx, accounts.OSRunner, vendor, env)
	switch {
	case err != nil:
		return translator.Text(clitext.AccountStateUnknown, nil)
	case connected:
		return translator.Text(clitext.AccountStateConnected, nil)
	}
	return translator.Text(clitext.AccountStateDisconnected, nil)
}

func addAccount(ctx context.Context, messages *clitext.Messages, store accounts.Store, vendor string, name contracts.ProfileName, input io.Reader, output io.Writer) error {
	lang := clitext.ClientLanguage()
	translator := messages.For(lang)
	dir, err := store.Create(vendor, name)
	if err != nil {
		return err
	}
	env, err := contracts.ProfileEnv(vendor, dir)
	if err != nil {
		return err
	}
	login, err := accounts.LoginCommand(ctx, vendor)
	if err != nil {
		return err
	}
	login.Env = contracts.MergeEnv(os.Environ(), env)
	login.Stdin, login.Stdout, login.Stderr = input, output, os.Stderr
	if runErr := login.Run(); runErr != nil {
		return messages.Failure(lang, runErr, clitext.AccountLoginFailed, clitext.Params{
			clitext.ParamVendor: vendor,
			clitext.ParamName:   string(name),
			clitext.ParamErr:    runErr.Error(),
		})
	}
	return writeText(output, translator, clitext.AccountAdded, clitext.Params{
		clitext.ParamVendor: vendor,
		clitext.ParamName:   string(name),
		clitext.ParamState:  loginState(ctx, translator, vendor, env),
	})
}

func removeAccount(messages *clitext.Messages, store accounts.Store, vendor string, name contracts.ProfileName, input io.Reader, output io.Writer) error {
	translator := messages.For(clitext.ClientLanguage())
	defaultName, err := store.Default(vendor)
	if err != nil {
		return err
	}
	users, err := host.ProfileUsers(vendor, string(name), defaultName == string(name))
	if err != nil {
		return err
	}
	if refuseErr := refuseProfileInUse(messages, vendor, name, users); refuseErr != nil {
		return refuseErr
	}
	prompt := translator.Text(clitext.AccountRemovePrompt, clitext.Printable(clitext.Params{clitext.ParamVendor: vendor, clitext.ParamName: string(name)}))
	if textErr := translator.Err(); textErr != nil {
		return textErr
	}
	if promptErr := emitf(output, "%s", prompt); promptErr != nil {
		return promptErr
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return messages.Failure(clitext.ClientLanguage(), err, clitext.AccountReadConfirmation, clitext.Params{clitext.ParamErr: err.Error()})
	}
	if !confirmed(answer) {
		return writeText(output, translator, clitext.AccountCancelled, nil)
	}
	return store.Remove(vendor, name, false)
}

func refuseProfileInUse(messages *clitext.Messages, vendor string, name contracts.ProfileName, users []string) error {
	if len(users) == 0 {
		return nil
	}
	return messages.Failure(clitext.ClientLanguage(), accounts.ErrInUse, clitext.AccountInUse, clitext.Params{
		clitext.ParamVendor:   vendor,
		clitext.ParamName:     string(name),
		clitext.ParamSessions: strings.Join(users, ", "),
	})
}

func warnResumingSessions(messages *clitext.Messages, vendor string, output io.Writer) error {
	names, err := host.ResumingSessions(vendor)
	if err != nil {
		return err
	}
	return warnResuming(messages, vendor, names, output)
}

func warnResuming(messages *clitext.Messages, vendor string, names []string, output io.Writer) error {
	if len(names) == 0 {
		return nil
	}
	translator := messages.For(clitext.ClientLanguage())
	return writeText(output, translator, clitext.AccountResuming, clitext.Params{
		clitext.ParamCount:    len(names),
		clitext.ParamVendor:   vendor,
		clitext.ParamSessions: strings.Join(names, ", "),
	})
}

func confirmed(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "oui", "o", "yes", "y":
		return true
	}
	return false
}
