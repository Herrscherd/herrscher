package discovery

import "time"

// BrowseWindow is how long the DNS-SD browse listens for announcements.
const BrowseWindow = 2 * time.Second

// DefaultSources returns the sources an operator's discovery reads: the local
// network, the tailnet and the ssh configuration at sshConfigPath.
func DefaultSources(sshConfigPath string) []Source {
	return []Source{
		MDNSSource{Window: BrowseWindow},
		NewTailscale(),
		SSHConfig{Path: sshConfigPath},
	}
}
