// Package forgecreds gives the host the credentials of its code forges: the
// token a user saves with herrscher connect, the token of the environment, or
// the login of the forge's own command line tool, in that order.
package forgecreds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

const (
	storeFileName = "forge-connections.json"
	storeDirMode  = 0o700
	storeFileMode = 0o600
	ownersOnly    = 0o077
	renewWindow   = 14 * 24 * time.Hour
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

// Expired reports whether the connection of r can no longer be renewed or used
// at now: its refresh token ran out, or, without a refresh token, its access
// token did.
func (r Record) Expired(now time.Time) bool {
	if r.RefreshToken != "" {
		return !r.RefreshExpiresAt.IsZero() && !now.Before(r.RefreshExpiresAt)
	}
	return !r.AccessExpiresAt.IsZero() && !now.Before(r.AccessExpiresAt)
}

// RenewSoon reports whether the refresh token of r runs out within the warning
// window that status announces.
func (r Record) RenewSoon(now time.Time) bool {
	return r.RefreshToken != "" && !r.RefreshExpiresAt.IsZero() && !now.Add(renewWindow).Before(r.RefreshExpiresAt)
}

type storeFile struct {
	Hosts map[string]Record `json:"hosts"`
}

// Store keeps the saved records of every forge host in one file of the host
// state directory. Writes are atomic and the file is 0600. Each change to the
// file runs under an exclusive lock shared by every process of that directory,
// and that lock is held only while the file is read, changed and written.
type Store struct {
	dir string
}

// Tx is the connections file as one change of Store.Update sees it. Its
// records are changed only through Put and Delete, so the file is saved only
// when a change was made.
type Tx struct {
	hosts map[string]Record
	dirty bool
}

// Get returns the record of host in the transaction and whether there is one.
func (t *Tx) Get(host contracts.ForgeHost) (Record, bool) {
	record, found := t.hosts[host.String()]
	return record, found
}

// Put sets the record of host, replacing any earlier one.
func (t *Tx) Put(host contracts.ForgeHost, record Record) {
	t.hosts[host.String()] = record
	t.dirty = true
}

// Delete removes the record of host and reports whether there was one.
func (t *Tx) Delete(host contracts.ForgeHost) bool {
	if _, found := t.hosts[host.String()]; !found {
		return false
	}
	delete(t.hosts, host.String())
	t.dirty = true
	return true
}

// NewStore keeps the forge connections file in dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Path returns the file the saved records live in.
func (s *Store) Path() string {
	return filepath.Join(s.dir, storeFileName)
}

func (s *Store) lockPath() string {
	return filepath.Join(s.dir, "."+storeFileName+".lock")
}

func (s *Store) renewLockPath(host contracts.ForgeHost) string {
	sum := sha256.Sum256([]byte(host.String()))
	return filepath.Join(s.dir, "."+storeFileName+"."+hex.EncodeToString(sum[:])+".renew.lock")
}

func (s *Store) lockRenewal(ctx context.Context, host contracts.ForgeHost) (func(), error) {
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	release, err := filelock.Acquire(ctx, s.renewLockPath(host))
	if err != nil {
		return nil, fmt.Errorf("lock forge renewal: %w", err)
	}
	return release, nil
}

func (s *Store) ensureDir() error {
	if err := os.MkdirAll(s.dir, storeDirMode); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	return nil
}

// Get returns the saved record of host and whether there is one.
func (s *Store) Get(host contracts.ForgeHost) (Record, bool, error) {
	file, err := s.load()
	if err != nil {
		return Record{}, false, err
	}
	record, found := file.Hosts[host.String()]
	return record, found, nil
}

// Hosts lists the forge hosts that have a saved record.
func (s *Store) Hosts() ([]string, error) {
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

// Update runs change under the lock of the connections file, on the records as
// they are at that moment, and saves them when change put or deleted one. The
// lock is held until change returns, so change must not wait on the network:
// a renewal takes the lock of its host first and calls Update only to record
// its result.
func (s *Store) Update(ctx context.Context, change func(*Tx) error) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	release, err := filelock.Acquire(ctx, s.lockPath())
	if err != nil {
		return fmt.Errorf("lock forge connections: %w", err)
	}
	defer release()
	file, err := s.load()
	if err != nil {
		return err
	}
	transaction := &Tx{hosts: file.Hosts}
	if err := change(transaction); err != nil {
		return err
	}
	if !transaction.dirty {
		return nil
	}
	return s.save(file)
}

// Put saves the record of host, replacing any earlier one.
func (s *Store) Put(ctx context.Context, host contracts.ForgeHost, record Record) error {
	return s.Update(ctx, func(transaction *Tx) error {
		transaction.Put(host, record)
		return nil
	})
}

// Delete removes the saved record of host and reports whether there was one.
// An unreadable file is removed whole, so that logout always clears it, and is
// reported as removed.
func (s *Store) Delete(ctx context.Context, host contracts.ForgeHost) (bool, error) {
	var removed bool
	err := s.Update(ctx, func(transaction *Tx) error {
		removed = transaction.Delete(host)
		return nil
	})
	if errors.Is(err, ErrStoreCorrupt) {
		if rmErr := os.Remove(s.Path()); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return false, fmt.Errorf("remove unreadable forge connections: %w", rmErr)
		}
		return true, nil
	}
	return removed, err
}

// Insecure reports whether the connections file can be read by users other
// than its owner. Windows file modes do not carry that meaning, so it reports
// false there. Nothing is repaired: the caller decides what to tell the user.
func (s *Store) Insecure() (bool, error) {
	if runtime.GOOS == "windows" {
		return false, nil
	}
	info, err := os.Stat(s.Path())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect forge connections: %w", err)
	}
	insecure := info.Mode().Perm()&ownersOnly != 0
	if insecure {
		slog.Warn("forge connections file readable by other users", "path", s.Path())
	}
	return insecure, nil
}

func (s *Store) load() (storeFile, error) {
	file := storeFile{Hosts: make(map[string]Record)}
	raw, err := os.ReadFile(s.Path())
	if errors.Is(err, fs.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return file, fmt.Errorf("read forge connections: %w", err)
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return storeFile{}, fmt.Errorf("%s: %w", s.Path(), ErrStoreCorrupt)
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
	return s.writeAtomic(raw)
}

func (s *Store) writeAtomic(data []byte) error {
	if err := atomicfile.Write(s.Path(), data, storeFileMode); err != nil {
		return fmt.Errorf("write forge connections: %w", err)
	}
	return nil
}
