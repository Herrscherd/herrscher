package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/host"
)

const accountArgCount = 3

var errAccountUsage = errors.New("usage: herrscher account list | add <vendor> <name> | remove <vendor> <name> | default <vendor> <name>")

func runAccount(ctx context.Context, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	store := accounts.Store{Root: accounts.DefaultRoot(home)}
	if len(args) == 0 {
		return errAccountUsage
	}
	switch args[0] {
	case "list":
		return listAccounts(ctx, store, os.Stdout)
	case "add":
		vendor, name, err := accountArgs(args)
		if err != nil {
			return err
		}
		return addAccount(ctx, store, vendor, name, os.Stdin, os.Stdout)
	case "remove":
		vendor, name, err := accountArgs(args)
		if err != nil {
			return err
		}
		return removeAccount(store, vendor, name, os.Stdin, os.Stdout)
	case "default":
		vendor, name, err := accountArgs(args)
		if err != nil {
			return err
		}
		if setErr := store.SetDefault(vendor, name); setErr != nil {
			return setErr
		}
		return emitf(os.Stdout, "%s default: %s\n", vendor, name)
	}
	return errAccountUsage
}

func accountArgs(args []string) (string, contracts.ProfileName, error) {
	if len(args) != accountArgCount {
		return "", "", errAccountUsage
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

func listAccounts(ctx context.Context, store accounts.Store, output io.Writer) error {
	profiles, err := store.List()
	if err != nil {
		return err
	}
	for _, vendor := range []string{contracts.VendorClaude, contracts.VendorCodex} {
		if err := emitf(output, "%-8s native     %s\n", vendor, loginState(ctx, vendor, nil)); err != nil {
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
			marker = " (default)"
		}
		if err := emitf(output, "%-8s %-10s %s%s\n", profile.Vendor, profile.Name, loginState(ctx, profile.Vendor, env), marker); err != nil {
			return err
		}
	}
	return nil
}

func loginState(ctx context.Context, vendor string, env map[string]string) string {
	connected, err := accounts.Status(ctx, accounts.OSRunner, vendor, env)
	switch {
	case err != nil:
		return "statut inconnu"
	case connected:
		return "connecté"
	}
	return "non connecté"
}

func addAccount(ctx context.Context, store accounts.Store, vendor string, name contracts.ProfileName, input io.Reader, output io.Writer) error {
	dir, err := store.Create(vendor, name)
	if err != nil {
		return err
	}
	env, err := contracts.ProfileEnv(vendor, dir)
	if err != nil {
		return err
	}
	argv, err := accounts.LoginArgv(vendor)
	if err != nil {
		return err
	}
	login := exec.CommandContext(ctx, argv[0], argv[1:]...)
	login.Env = contracts.MergeEnv(os.Environ(), env)
	login.Stdin, login.Stdout, login.Stderr = input, output, os.Stderr
	if runErr := login.Run(); runErr != nil {
		return fmt.Errorf("%s login for %s: %w (the profile directory is kept, run add again or remove it)", vendor, name, runErr)
	}
	return emitf(output, "%s/%s: %s\n", vendor, name, loginState(ctx, vendor, env))
}

func removeAccount(store accounts.Store, vendor string, name contracts.ProfileName, input io.Reader, output io.Writer) error {
	inUse, err := host.ProfileInUse(vendor, string(name))
	if err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("%s/%s: %w", vendor, name, accounts.ErrInUse)
	}
	if promptErr := emitf(output, "Supprimer le profil %s/%s et son dossier de connexion ? (oui/non) ", vendor, name); promptErr != nil {
		return promptErr
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if !confirmed(answer) {
		return emitf(output, "annulé\n")
	}
	return store.Remove(vendor, name, false)
}

func confirmed(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "oui", "o", "yes", "y":
		return true
	}
	return false
}
