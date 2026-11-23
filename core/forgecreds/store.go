// Package forgecreds gives the host the credentials of its code forges: the
// token a user saves with herrscher connect, the token of the environment, or
// the login of the forge's own command line tool, in that order.
package forgecreds

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	storeFileName = "forge-connections.json"
	storeDirMode  = 0o700
	storeFileMode = 0o600
)

// ErrStoreCorrupt reports a forge connections file that does not parse.
var ErrStoreCorrupt = errors.New("forge connections file is not valid")

// Record is what herrscher connect stores for one forge host. The tokens are
// secrets: a Record is never printed, logged or put in an error.
type Record struct {
	Login            string    `json:"login,omitempty"`
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at,omitzero"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at,omitzero"`
	Rejected         bool      `json:"rejected,omitempty"`
}

type storeFile struct {
	Hosts map[string]Record `json:"hosts"`
}

// Store keeps the saved records of every forge host in one file of the host
// state directory, written atomically with mode 0600.
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore keeps the forge connections file in dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) path() string {
	return filepath.Join(s.dir, storeFileName)
}

// Get returns the saved record of host and whether there is one.
func (s *Store) Get(host contracts.ForgeHost) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return Record{}, false, err
	}
	record, found := file.Hosts[host.String()]
	return record, found, nil
}

// Hosts lists the forge hosts that have a saved record.
func (s *Store) Hosts() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(file.Hosts))
	for name := range file.Hosts {
		names = append(names, name)
	}
	return names, nil
}

// Put saves the record of host, replacing any earlier one.
func (s *Store) Put(host contracts.ForgeHost, record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return err
	}
	file.Hosts[host.String()] = record
	return s.save(file)
}

// Reject marks the saved token of host as refused by the forge. The token is
// kept so that status can report it, and it is no longer used.
func (s *Store) Reject(host contracts.ForgeHost) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return err
	}
	record, found := file.Hosts[host.String()]
	if !found {
		return nil
	}
	record.Rejected = true
	file.Hosts[host.String()] = record
	return s.save(file)
}

// Delete removes the saved record of host and reports whether there was one.
func (s *Store) Delete(host contracts.ForgeHost) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return false, err
	}
	if _, found := file.Hosts[host.String()]; !found {
		return false, nil
	}
	delete(file.Hosts, host.String())
	return true, s.save(file)
}

func (s *Store) load() (storeFile, error) {
	file := storeFile{Hosts: make(map[string]Record)}
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return file, fmt.Errorf("read forge connections: %w", err)
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return storeFile{}, fmt.Errorf("%s: %w", s.path(), ErrStoreCorrupt)
	}
	if file.Hosts == nil {
		file.Hosts = make(map[string]Record)
	}
	return file, nil
}

func (s *Store) save(file storeFile) error {
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode forge connections: %w", err)
	}
	if err := os.MkdirAll(s.dir, storeDirMode); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	return writeAtomic(s.path(), raw)
}

func writeAtomic(path string, data []byte) error {
	staging, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("stage forge connections: %w", err)
	}
	if stageErr := writeStaged(staging, data); stageErr != nil {
		discardStaged(staging)
		return stageErr
	}
	if renameErr := os.Rename(staging.Name(), path); renameErr != nil {
		discardStaged(staging)
		return fmt.Errorf("replace forge connections: %w", renameErr)
	}
	return nil
}

func writeStaged(staging *os.File, data []byte) error {
	if err := staging.Chmod(storeFileMode); err != nil {
		return fmt.Errorf("protect forge connections: %w", err)
	}
	if _, err := staging.Write(data); err != nil {
		return fmt.Errorf("write forge connections: %w", err)
	}
	if err := staging.Close(); err != nil {
		return fmt.Errorf("write forge connections: %w", err)
	}
	return nil
}

func discardStaged(staging *os.File) {
	if err := staging.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		slog.Warn("close staged forge connections", "err", err)
	}
	if err := os.Remove(staging.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("remove staged forge connections", "err", err)
	}
}
