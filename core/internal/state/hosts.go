package state

// LocalHost is the name of this machine. It is implicit: it always exists, it
// is never a stored record, and its workspace is the daemon's own.
const LocalHost = "local"

// Host is a named place where a session's process runs.
type Host struct {
	Name      string `json:"name"`
	SSH       string `json:"ssh,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Bin       string `json:"bin,omitempty"`
	Version   string `json:"version,omitempty"`
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
	for _, host := range s.Hosts {
		if host.Name == name {
			return host, true
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
