package discovery

import "time"

// SourceDeadline bounds one source, and every source of one discovery together: a
// source that does not answer in time is reported unavailable instead of holding
// the whole report.
const SourceDeadline = 2 * time.Second

// BrowseWindow is how long the DNS-SD browse listens for announcements.
const BrowseWindow = SourceDeadline

// DefaultSources returns the sources an operator's discovery reads: the local
// network, the tailnet and the ssh configuration at sshConfigPath.
func DefaultSources(sshConfigPath string) []Source {
	return []Source{
		MDNSSource{Window: BrowseWindow},
		NewTailscale(),
		SSHConfig{Path: sshConfigPath},
	}
}
