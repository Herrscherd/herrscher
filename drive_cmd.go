package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/drives"
	"github.com/Herrscherd/herrscher/core/host"
)

const (
	driveVerb       = "drive"
	driveSubAdd     = "add"
	driveFlagTo     = "--to"
	driveFlagFrom   = "--from"
	driveKeyEnvFlag = "--secret-env"
	driveConfigMode = 0o600
)

var (
	errDriveUsage         = errors.New("drive usage")
	errDriveSecretMissing = errors.New("no secret access key")
	errDriveAccount       = errors.New("an r2 drive needs --account")
	errDriveAccessKey     = errors.New("a key drive needs --access-key")
)

type driveAddRequest struct {
	name      drives.Name
	kind      drives.Kind
	folders   []string
	access    drives.Access
	accessKey string
	secretEnv string
	region    string
	endpoint  string
	provider  string
	account   string
}

func runDrive(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	if len(args) > 0 && args[0] == driveSubAdd {
		return runDriveAdd(ctx, messages, args[1:])
	}
	if os.Getenv(host.SessionCommandSocketVar) == "" {
		args = absoluteLocalPaths(args)
	}
	return runForwarded(ctx, layout, messages, hostMessages, driveVerb, args)
}

func absoluteLocalPaths(args []string) []string {
	out := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		token := args[index]
		name, value, inline := strings.Cut(token, "=")
		if name != driveFlagTo && name != driveFlagFrom {
			out = append(out, token)
			continue
		}
		if !inline {
			if index+1 >= len(args) {
				out = append(out, token)
				continue
			}
			index++
			value = args[index]
		}
		out = append(out, name+"="+absoluteOf(value))
	}
	return out
}

func absoluteOf(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func driveUsage(messages *clitext.Messages, lang i18n.Language) error {
	return renderError(messages, lang, errDriveUsage, clitext.DriveUsage, clitext.Params{clitext.ParamKind: strings.Join(drives.KindNames(), "|")})
}

func parseDriveAdd(messages *clitext.Messages, lang i18n.Language, args []string) (driveAddRequest, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return driveAddRequest{}, driveUsage(messages, lang)
	}
	flags := flag.NewFlagSet(driveVerb+" "+driveSubAdd, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	kind := flags.String("kind", "", "")
	folders := flags.String("folders", "", "")
	agents := flags.String("agents", "none", "")
	request := driveAddRequest{}
	flags.StringVar(&request.accessKey, "access-key", "", "")
	flags.StringVar(&request.secretEnv, "secret-env", "", "")
	flags.StringVar(&request.region, "region", "", "")
	flags.StringVar(&request.endpoint, "endpoint", "", "")
	flags.StringVar(&request.provider, "provider", "", "")
	flags.StringVar(&request.account, "account", "", "")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() > 0 {
		return driveAddRequest{}, driveUsage(messages, lang)
	}
	var err error
	if request.name, err = drives.ParseName(args[0]); err != nil {
		return driveAddRequest{}, err
	}
	if request.kind, err = drives.ParseKind(*kind); err != nil {
		return driveAddRequest{}, err
	}
	if request.access, err = drives.ParseAccess(*agents); err != nil {
		return driveAddRequest{}, err
	}
	if request.folders, err = parseFolders(*folders); err != nil {
		return driveAddRequest{}, err
	}
	return request, nil
}

func parseFolders(raw string) ([]string, error) {
	var folders []string
	for field := range strings.SplitSeq(raw, ",") {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		folder, err := drives.ParsePath(trimmed)
		if err != nil {
			return nil, err
		}
		if !folder.IsRoot() {
			folders = append(folders, folder.String())
		}
	}
	return folders, nil
}

func runDriveAdd(ctx context.Context, messages *clitext.Messages, args []string) error {
	lang := clitext.ClientLanguage()
	request, err := parseDriveAdd(messages, lang, args)
	if err != nil {
		return err
	}
	catalog := drives.Open(host.DrivesDir(host.DefaultStatePath()))
	if err := refuseTakenDrive(catalog, request.name); err != nil {
		return err
	}
	drive := drives.Drive{Name: request.name.String(), Kind: request.kind, Folders: request.folders, Default: request.access}
	if err := connectDrive(ctx, messages, lang, catalog, request, drive); err != nil {
		return err
	}
	return writeText(os.Stdout, messages.For(lang), clitext.DriveAdded, clitext.Params{clitext.ParamDrive: drive.Name, clitext.ParamKind: drive.Kind.String(), clitext.ParamAccess: drive.Default.String()})
}

func refuseTakenDrive(catalog drives.Catalog, name drives.Name) error {
	if err := catalog.Ensure(); err != nil {
		return err
	}
	_, findErr := catalog.Find(name.String())
	if findErr == nil {
		return drives.ErrDriveExists
	}
	if _, missing := errors.AsType[*drives.NotFoundError](findErr); !missing {
		return findErr
	}
	taken, err := catalog.HasRemote(name)
	if err != nil {
		return err
	}
	if taken {
		return drives.ErrRemoteExists
	}
	return nil
}

func connectDrive(ctx context.Context, messages *clitext.Messages, lang i18n.Language, catalog drives.Catalog, request driveAddRequest, drive drives.Drive) error {
	if !request.kind.SignsInWithOAuth() {
		remote, err := keyRemote(messages, lang, request)
		if err != nil {
			return err
		}
		return catalog.AddKeyDrive(ctx, drive, remote)
	}
	if err := writeText(os.Stdout, messages.For(lang), clitext.DriveSignInStarting, clitext.Params{clitext.ParamKind: request.kind.String()}); err != nil {
		return err
	}
	rclone := host.DriveRclone(catalog)
	terminal := drives.Terminal{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	if err := rclone.CreateRemote(ctx, drive.Name, request.kind.RcloneType(), drives.OAuthParams(request.kind), terminal); err != nil {
		return err
	}
	if err := os.Chmod(catalog.RcloneConfig(), driveConfigMode); err != nil {
		return err
	}
	if err := catalog.Add(ctx, drive); err != nil {
		return errors.Join(err, rclone.DeleteRemote(context.WithoutCancel(ctx), drive.Name))
	}
	return nil
}

func keyRemote(messages *clitext.Messages, lang i18n.Language, request driveAddRequest) (drives.KeyRemote, error) {
	if request.accessKey == "" {
		return drives.KeyRemote{}, renderError(messages, lang, errDriveAccessKey, clitext.DriveAccessKeyMissing, nil)
	}
	remote := drives.KeyRemote{Kind: request.kind, Provider: request.provider, AccessKeyID: request.accessKey, Region: request.region, Endpoint: request.endpoint}
	if request.kind == drives.KindR2 {
		if request.account == "" {
			return drives.KeyRemote{}, renderError(messages, lang, errDriveAccount, clitext.DriveAccountRequired, nil)
		}
		remote.Endpoint = drives.R2Endpoint(request.account)
	}
	secret, err := driveSecret(messages, lang, request.secretEnv)
	if err != nil {
		return drives.KeyRemote{}, err
	}
	remote.Secret = secret
	return remote, nil
}

func driveSecret(messages *clitext.Messages, lang i18n.Language, secretEnv string) (string, error) {
	if secretEnv != "" {
		if value := strings.TrimSpace(os.Getenv(secretEnv)); value != "" {
			return value, nil
		}
		return "", renderError(messages, lang, errDriveSecretMissing, clitext.DriveSecretMissing, clitext.Params{clitext.ParamName: secretEnv})
	}
	stdin := int(os.Stdin.Fd())
	if !term.IsTerminal(stdin) {
		return "", renderError(messages, lang, errDriveSecretMissing, clitext.DriveSecretMissing, clitext.Params{clitext.ParamName: driveKeyEnvFlag})
	}
	if err := emitf(os.Stderr, "%s", messages.For(lang).Text(clitext.DriveSecretPrompt, nil)); err != nil {
		return "", err
	}
	typed, readErr := term.ReadPassword(stdin)
	if err := errors.Join(readErr, emitf(os.Stderr, "\n")); err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(typed))
	if secret == "" {
		return "", renderError(messages, lang, errDriveSecretMissing, clitext.DriveSecretMissing, clitext.Params{clitext.ParamName: driveKeyEnvFlag})
	}
	return secret, nil
}
