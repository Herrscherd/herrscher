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

const sshWildcards = "*?!"

// SSHConfig proposes the concrete host aliases of an ssh configuration file as
// places a session can run. Patterns with wildcards or negations are not hosts
// and are skipped. Files named by Include directives are not followed.
type SSHConfig struct {
	Path string
}

// Name returns the label of the ssh configuration source.
func (SSHConfig) Name() string {
	return "ssh-config"
}

// Find reads the configuration file. A missing file is an empty source.
func (source SSHConfig) Find(_ context.Context) ([]Candidate, error) {
	file, err := os.Open(source.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	found, readErr := candidatesFromSSHConfig(file)
	return found, errors.Join(readErr, file.Close())
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
