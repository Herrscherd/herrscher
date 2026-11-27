package drives

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
)

const (
	providerCloudflare = "Cloudflare"
	providerAWS        = "AWS"
	regionAuto         = "auto"
	secretsFile        = "secrets.json"
	remoteEnvPrefix    = "RCLONE_CONFIG_"
	secretSetting      = "secret_access_key"
)

var (
	// ErrRemoteExists reports a name herrscher's rclone configuration already holds.
	ErrRemoteExists = errors.New("herrscher's rclone configuration already holds a remote of that name")
	// ErrNotKeyKind reports a key remote asked for a kind that signs in with OAuth.
	ErrNotKeyKind = errors.New("this kind signs in through the browser, not with an access key")
	// ErrSecretMissing reports a key drive whose secret herrscher no longer holds.
	ErrSecretMissing = errors.New("the secret of this drive is missing: remove it and add it again")
)

// BadSettingError reports a value herrscher will not hand to rclone.
type BadSettingError struct {
	Setting string
}

func (e *BadSettingError) Error() string {
	return e.Setting + " is empty or holds a space or a control character"
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

// AddKeyDrive records an S3 or R2 drive. The remote never enters the rclone
// configuration: its settings are kept with the drive, its secret in a file of
// its own readable by the owner alone, and both reach rclone through the
// environment of each run, so a token refresh that rewrites the rclone
// configuration can never lose it.
func (c Catalog) AddKeyDrive(ctx context.Context, drive Drive, remote KeyRemote) error {
	settings, err := keySettings(remote)
	if err != nil {
		return err
	}
	if !safeSettingValue(remote.Secret) {
		return &BadSettingError{Setting: secretSetting}
	}
	drive.Settings = settings
	return c.edit(ctx, func(all []Drive) ([]Drive, error) {
		if slices.ContainsFunc(all, func(d Drive) bool { return d.Name == drive.Name }) {
			return nil, ErrDriveExists
		}
		secrets, loadErr := c.loadSecrets()
		if loadErr != nil {
			return nil, loadErr
		}
		secrets[drive.Name] = remote.Secret
		if saveErr := c.saveSecrets(secrets); saveErr != nil {
			return nil, saveErr
		}
		return append(all, drive), nil
	})
}

// Environment is what rclone needs in its environment to reach drive: the
// settings and the secret of a key drive, nothing for an OAuth drive, whose
// token lives in the rclone configuration.
func (c Catalog) Environment(drive Drive) ([]string, error) {
	if len(drive.Settings) == 0 {
		return nil, nil
	}
	secrets, err := c.loadSecrets()
	if err != nil {
		return nil, err
	}
	secret, ok := secrets[drive.Name]
	if !ok {
		return nil, ErrSecretMissing
	}
	prefix := remoteEnvPrefix + strings.ToUpper(drive.Name) + "_"
	env := make([]string, 0, len(drive.Settings)+1)
	for _, key := range slices.Sorted(maps.Keys(drive.Settings)) {
		env = append(env, prefix+strings.ToUpper(key)+"="+drive.Settings[key])
	}
	return append(env, prefix+strings.ToUpper(secretSetting)+"="+secret), nil
}

func (c Catalog) loadSecrets() (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(c.dir, secretsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, err
	}
	secrets := make(map[string]string)
	if err := json.Unmarshal(raw, &secrets); err != nil {
		return nil, err
	}
	return secrets, nil
}

func (c Catalog) saveSecrets(secrets map[string]string) error {
	raw, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(c.dir, secretsFile), raw, fileMode)
}

func (c Catalog) forgetSecret(name string) error {
	secrets, err := c.loadSecrets()
	if err != nil {
		return err
	}
	if _, ok := secrets[name]; !ok {
		return nil
	}
	delete(secrets, name)
	return c.saveSecrets(secrets)
}

func keySettings(remote KeyRemote) (map[string]string, error) {
	var settings map[string]string
	switch remote.Kind {
	case KindR2:
		settings = map[string]string{"provider": providerCloudflare, "region": regionAuto, "endpoint": remote.Endpoint, "no_check_bucket": "true"}
	case KindS3:
		provider := remote.Provider
		if provider == "" {
			provider = providerAWS
		}
		settings = map[string]string{"provider": provider}
		for key, value := range map[string]string{"region": remote.Region, "endpoint": remote.Endpoint} {
			if value != "" {
				settings[key] = value
			}
		}
	case KindGoogleDrive, KindDropbox, KindOneDrive:
	}
	if settings == nil {
		return nil, ErrNotKeyKind
	}
	settings["type"] = kindS3Name
	settings["env_auth"] = "false"
	settings["access_key_id"] = remote.AccessKeyID
	for key, value := range settings {
		if !safeSettingValue(value) {
			return nil, &BadSettingError{Setting: key}
		}
	}
	return settings, nil
}

func safeSettingValue(value string) bool {
	return value != "" && !strings.ContainsFunc(value, func(char rune) bool {
		return unicode.IsSpace(char) || unicode.IsControl(char)
	})
}
