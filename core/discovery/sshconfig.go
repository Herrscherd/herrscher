package discovery

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/kevinburke/ssh_config"
)

const (
	sshWildcards      = "*?!"
	sshConfigMaxBytes = 1 << 20
)

var (
	// ErrSSHConfigNotRegular reports an ssh configuration path that is not a regular
	// file, such as a named pipe that would block the read forever.
	ErrSSHConfigNotRegular = errors.New("ssh configuration is not a regular file")
	// ErrSSHConfigTooLarge reports an ssh configuration larger than the source reads.
	ErrSSHConfigTooLarge = errors.New("ssh configuration is too large to read")
)

// SSHConfig proposes the concrete host aliases of an ssh configuration file as
// places a session can run. Patterns with wildcards or negations are not hosts
// and are skipped. Files named by Include directives are not followed. A missing
// file is an empty source; a path that is not a regular file, or a file above one
// megabyte, is unavailable.
type SSHConfig struct {
	Path string
}

// Name returns the label of the ssh configuration source.
func (SSHConfig) Name() string {
	return "ssh-config"
}

// Find reads the configuration file. A missing file is an empty source. The file is
// opened without blocking, so a named pipe is refused instead of waiting for a writer.
func (source SSHConfig) Find(_ context.Context) ([]Candidate, error) {
	file, err := openSSHConfig(source.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	found, readErr := readSSHConfig(file)
	return found, errors.Join(readErr, file.Close())
}

func readSSHConfig(file *os.File) ([]Candidate, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrSSHConfigNotRegular
	}
	if info.Size() > sshConfigMaxBytes {
		return nil, ErrSSHConfigTooLarge
	}
	return candidatesFromSSHConfig(io.LimitReader(file, sshConfigMaxBytes))
}

func candidatesFromSSHConfig(reader io.Reader) ([]Candidate, error) {
	config, err := ssh_config.Decode(reader)
	if err != nil {
		return nil, err
	}
	var found []Candidate
	for _, host := range config.Hosts {
		for _, pattern := range host.Patterns {
			alias := plainText(pattern.String())
			if strings.ContainsAny(alias, sshWildcards) {
				continue
			}
			found = append(found, Candidate{
				Name:      alias,
				Address:   plainText(hostNameOf(config, alias)),
				Route:     RouteSSH,
				SSHTarget: alias,
			})
		}
	}
	return found, nil
}

func hostNameOf(config *ssh_config.Config, alias string) string {
	hostName, err := config.Get(alias, "HostName")
	if err != nil || hostName == "" {
		return alias
	}
	return hostName
}
