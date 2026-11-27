package drives

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
)

const (
	providerCloudflare = "Cloudflare"
	providerAWS        = "AWS"
	regionAuto         = "auto"
	firstPrintable     = '!'
	lastPrintable      = '~'
)

// ErrRemoteExists reports a remote the rclone configuration of herrscher already holds.
var ErrRemoteExists = errors.New("herrscher's rclone configuration already holds a remote of that name")

// ErrNotKeyKind reports a key remote asked for a kind that signs in with OAuth.
var ErrNotKeyKind = errors.New("this kind signs in through the browser, not with an access key")

// BadSettingError reports a value herrscher will not write into the rclone configuration.
type BadSettingError struct {
	Setting string
}

func (e *BadSettingError) Error() string {
	return e.Setting + " is empty or holds a space, a quote, a backslash, a bracket, # or ;"
}

// KeyRemote is what an S3 or R2 remote needs. Provider is the S3 provider
// name rclone knows, AWS when empty; R2 ignores it.
type KeyRemote struct {
	Kind        Kind
	Provider    string
	AccessKeyID string
	Secret      string
	Region      string
	Endpoint    string
}

// OAuthParams is what rclone config create needs beside the type for an OAuth kind.
func OAuthParams(kind Kind) []string {
	if kind == KindGoogleDrive {
		return []string{"scope=drive"}
	}
	return nil
}

// R2Endpoint is the endpoint of the R2 storage of a Cloudflare account.
func R2Endpoint(account string) string {
	return "https://" + account + ".r2.cloudflarestorage.com"
}

// HasRemote reports whether the rclone configuration of herrscher holds name.
func (c Catalog) HasRemote(name Name) (bool, error) {
	raw, err := os.ReadFile(c.RcloneConfig())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	header := "[" + name.String() + "]"
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == header {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// AddKeyRemote appends an S3 or R2 remote to the rclone configuration of
// herrscher. The secret is written there alone, readable by its owner.
func (c Catalog) AddKeyRemote(ctx context.Context, name Name, remote KeyRemote) error {
	section, err := keySection(name, remote)
	if err != nil {
		return err
	}
	release, err := c.hold(ctx)
	if err != nil {
		return err
	}
	defer release()
	exists, err := c.HasRemote(name)
	if err != nil {
		return err
	}
	if exists {
		return ErrRemoteExists
	}
	file, err := os.OpenFile(c.RcloneConfig(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(section)
	return errors.Join(writeErr, file.Close())
}

func keySection(name Name, remote KeyRemote) (string, error) {
	settings, err := keySettings(remote)
	if err != nil {
		return "", err
	}
	var section strings.Builder
	section.WriteString("\n[" + name.String() + "]\n")
	for _, setting := range settings {
		if setting.value == "" && !setting.required {
			continue
		}
		if !safeSettingValue(setting.value) {
			return "", &BadSettingError{Setting: setting.key}
		}
		section.WriteString(setting.key + " = " + setting.value + "\n")
	}
	return section.String(), nil
}

type setting struct {
	key      string
	value    string
	required bool
}

func keySettings(remote KeyRemote) ([]setting, error) {
	switch remote.Kind {
	case KindR2:
		return []setting{
			{key: "type", value: kindS3Name, required: true},
			{key: "provider", value: providerCloudflare, required: true},
			{key: "env_auth", value: "false", required: true},
			{key: "access_key_id", value: remote.AccessKeyID, required: true},
			{key: "secret_access_key", value: remote.Secret, required: true},
			{key: "region", value: regionAuto, required: true},
			{key: "endpoint", value: remote.Endpoint, required: true},
			{key: "no_check_bucket", value: "true", required: true},
		}, nil
	case KindS3:
		provider := remote.Provider
		if provider == "" {
			provider = providerAWS
		}
		return []setting{
			{key: "type", value: kindS3Name, required: true},
			{key: "provider", value: provider, required: true},
			{key: "env_auth", value: "false", required: true},
			{key: "access_key_id", value: remote.AccessKeyID, required: true},
			{key: "secret_access_key", value: remote.Secret, required: true},
			{key: "region", value: remote.Region},
			{key: "endpoint", value: remote.Endpoint},
		}, nil
	case KindGoogleDrive, KindDropbox, KindOneDrive:
	}
	return nil, ErrNotKeyKind
}

func safeSettingValue(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < firstPrintable || char > lastPrintable || strings.ContainsRune(`#;"'\[]`, char) {
			return false
		}
	}
	return true
}
