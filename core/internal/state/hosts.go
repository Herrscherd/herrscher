package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// LocalHost is the name of this machine. It is implicit: it always exists, it
// is never a stored record, and its workspace is the daemon's own.
const LocalHost = "local"

// ErrUnknownFullPolicy reports a when-full setting that is neither wait nor refuse.
var ErrUnknownFullPolicy = errors.New("when-full must be wait or refuse")

// FullPolicy says what a new session does when its host is full or unreachable.
type FullPolicy string

const (
	// FullRefuse fails the session at once. It is the default: a host that is full
	// is not a reason to queue sessions without the operator asking for it.
	FullRefuse FullPolicy = "refuse"
	// FullWait holds the session until a slot frees on the host or the wait limit passes.
	FullWait FullPolicy = "wait"
)

// ParseFullPolicy reads a when-full setting. An empty value is the default.
func ParseFullPolicy(text string) (FullPolicy, error) {
	switch FullPolicy(text) {
	case "", FullRefuse:
		return FullRefuse, nil
	case FullWait:
		return FullWait, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownFullPolicy, text)
	}
}

// Host is a named place where a session's process runs.
type Host struct {
	Name      string `json:"name"`
	SSH       string `json:"ssh,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Bin       string `json:"bin,omitempty"`
	Version   string `json:"version,omitempty"`
	GOOS      string `json:"goos,omitempty"`
	GOARCH    string `json:"goarch,omitempty"`
	// HostKey is the SHA256 fingerprint of the ssh host key the operator confirmed. Empty on a host registered before pinning existed.
	HostKey string `json:"hostkey,omitempty"`
	// KnownHosts is the dedicated known_hosts file that pins HostKey for every connection to this host.
	KnownHosts string `json:"knownhosts,omitempty"`
	// KeyChanged holds the fingerprint a check saw in place of HostKey. While set, the host takes no new session.
	KeyChanged string `json:"keychanged,omitempty"`
	// BinSHA256 is the digest of the herrscher binary provisioned on the host, checked at every launch.
	BinSHA256 string `json:"binsha256,omitempty"`
	// Projects lists the projects a session may use on this host. Empty allows every project.
	Projects []string `json:"projects,omitempty"`
	// MaxSessions caps the sessions running on this host at once. Zero means no cap.
	MaxSessions int        `json:"maxsessions,omitempty"`
	WhenFull    FullPolicy `json:"whenfull,omitempty"`
	// Load is the 1-minute load average the last check read, and LoadAt when.
	Load   *float64   `json:"load,omitempty"`
	LoadAt *time.Time `json:"loadat,omitempty"`
}

// KnownHostsFile names the dedicated known_hosts file of host name, beside the state file.
func (s *State) KnownHostsFile(name string) string {
	return filepath.Join(filepath.Dir(s.path), "known_hosts", knownHostsStem(name)+".known_hosts")
}

// SnapshotHosts returns a copy of the registered hosts. A copy, not the slice:
// callers iterate it outside the mutex.
func (s *State) SnapshotHosts() []Host {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Host(nil), s.Hosts...)
}

// FindHost returns the record named name. LocalHost is never one: it needs no
// record, and giving it one would let an operator redefine what "here" means.
func (s *State) FindHost(name string) (Host, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Hosts {
		if s.Hosts[idx].Name == name {
			return s.Hosts[idx], true
		}
	}
	return Host{}, false
}

// PutHost adds or replaces a host, by name.
func (s *State) PutHost(host Host) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Hosts {
		if s.Hosts[idx].Name == host.Name {
			s.Hosts[idx] = host
			return s.saveLocked()
		}
	}
	s.Hosts = append(s.Hosts, host)
	return s.saveLocked()
}

// RemoveHost drops a host and reports whether it was there.
func (s *State) RemoveHost(name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Hosts {
		if s.Hosts[idx].Name == name {
			s.Hosts = append(s.Hosts[:idx], s.Hosts[idx+1:]...)
			return true, s.saveLocked()
		}
	}
	return false, nil
}

// SessionsOn counts the live sessions placed on host name. Archived sessions hold
// no process, so they take no slot.
func (s *State) SessionsOn(name string) int {
	count := 0
	sessions := s.SnapshotSessions()
	for idx := range sessions {
		if sessions[idx].Host == name && !sessions[idx].Archived {
			count++
		}
	}
	return count
}

func knownHostsStem(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}
