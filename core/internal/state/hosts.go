package state

// LocalHost is the name of this machine. It is implicit: it always exists, it
// is never a stored record, and its workspace is the daemon's own.
const LocalHost = "local"

// Host is a named place where a session's process runs.
type Host struct {
	Name      string `json:"name"`
	SSH       string `json:"ssh,omitempty"`       // user@machine; "" = local
	Workspace string `json:"workspace,omitempty"` // abs path over there
	Bin       string `json:"bin,omitempty"`       // abs path to herrscher over there
	Version   string `json:"version,omitempty"`   // short commit provisioned
	GOOS      string `json:"goos,omitempty"`
	GOARCH    string `json:"goarch,omitempty"`
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
	for _, h := range s.Hosts {
		if h.Name == name {
			return h, true
		}
	}
	return Host{}, false
}

// PutHost adds or replaces a host, by name.
func (s *State) PutHost(h Host) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Hosts {
		if s.Hosts[i].Name == h.Name {
			s.Hosts[i] = h
			return s.saveLocked()
		}
	}
	s.Hosts = append(s.Hosts, h)
	return s.saveLocked()
}

// RemoveHost drops a host and reports whether it was there.
func (s *State) RemoveHost(name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Hosts {
		if s.Hosts[i].Name == name {
			s.Hosts = append(s.Hosts[:i], s.Hosts[i+1:]...)
			return true, s.saveLocked()
		}
	}
	return false, nil
}
