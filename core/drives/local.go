package drives

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	localFileMode  = os.FileMode(0o600)
	localDirMode   = os.FileMode(0o700)
	tempNameBytes  = 8
	tempNamePrefix = ".herrscher-drive-"
	stagingStale   = time.Hour
)

var (
	// ErrLocalPathProtected reports a session path under .git or a file herrscher materializes for the agent.
	ErrLocalPathProtected = errors.New("the path is under .git or a file herrscher writes for the agent, which a drive never reads into or sends")
	// ErrLocalShortName reports a session path with a segment shaped like a Windows short name, such as GIT~1, which could name a protected folder.
	ErrLocalShortName = errors.New("a segment holds ~ and a digit, the shape of a Windows short name, which could name a protected folder")
	// ErrLocalSymlink reports a session path that goes through a symbolic link.
	ErrLocalSymlink = errors.New("the path goes through a symbolic link")
)

func (s Service) checkLocal(caller Caller, raw string) error {
	if reason := characterFault(raw); reason != "" {
		return &InvalidPathError{Path: raw, Reason: reason}
	}
	if caller.isOperator() {
		if !filepath.IsAbs(raw) {
			return ErrLocalPathOutside
		}
		return nil
	}
	if caller.Worktree == "" {
		return ErrNoWorktree
	}
	if !filepath.IsLocal(raw) {
		return ErrLocalPathOutside
	}
	segments := strings.Split(filepath.ToSlash(filepath.Clean(raw)), "/")
	for index, segment := range segments {
		if looksShortName(segment) {
			return ErrLocalShortName
		}
		protected := strings.EqualFold(segment, gitSegment) || (index == 0 && slices.ContainsFunc(s.protected, func(name string) bool { return strings.EqualFold(name, segment) }))
		if protected {
			return ErrLocalPathProtected
		}
	}
	return nil
}

func looksShortName(segment string) bool {
	tilde := strings.IndexByte(segment, '~')
	return tilde >= 0 && tilde+1 < len(segment) && segment[tilde+1] >= '0' && segment[tilde+1] <= '9'
}

func (s Service) localRoot(caller Caller, raw string) (*os.Root, string, error) {
	if err := s.checkLocal(caller, raw); err != nil {
		return nil, "", err
	}
	if caller.isOperator() {
		root, err := os.OpenRoot(filepath.Dir(raw))
		return root, filepath.Base(raw), err
	}
	root, err := os.OpenRoot(caller.Worktree)
	if err != nil {
		return nil, "", err
	}
	name := filepath.Clean(raw)
	if err := refuseLinkedParents(root, name); err != nil {
		closeRoot(root)
		return nil, "", err
	}
	return root, name, nil
}

func refuseLinkedParents(root *os.Root, name string) error {
	parent := ""
	for _, segment := range strings.Split(filepath.Dir(name), string(filepath.Separator)) {
		if segment == "." {
			return nil
		}
		parent = filepath.Join(parent, segment)
		info, err := root.Lstat(parent)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return ErrLocalSymlink
		}
	}
	return nil
}

func openSource(root *os.Root, name string) (*os.File, int64, error) {
	file, err := root.OpenFile(name, sourceOpenFlags, 0)
	if isSymlinkRefusal(err) {
		return nil, 0, ErrLocalSymlink
	}
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		closeLocal(file)
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		closeLocal(file)
		return nil, 0, ErrNotRegularFile
	}
	return file, info.Size(), nil
}

func createStaging(root *os.Root, name string) (*os.File, string, error) {
	if dir := filepath.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, localDirMode); err != nil {
			return nil, "", err
		}
	}
	sweepStaleStaging(root, filepath.Dir(name))
	suffix := make([]byte, tempNameBytes)
	if _, err := rand.Read(suffix); err != nil {
		return nil, "", err
	}
	staging := filepath.Join(filepath.Dir(name), tempNamePrefix+hex.EncodeToString(suffix))
	file, err := root.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL|stagingOpenFlags, localFileMode)
	return file, staging, err
}

func sweepStaleStaging(root *os.Root, dir string) {
	folder, err := root.Open(dir)
	if err != nil {
		return
	}
	defer closeLocal(folder)
	entries, err := folder.ReadDir(-1)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), tempNamePrefix) || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err == nil && time.Since(info.ModTime()) > stagingStale {
			discardStaging(root, filepath.Join(dir, entry.Name()))
		}
	}
}

func discardStaging(root *os.Root, staging string) {
	if err := root.Remove(staging); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("remove drive staging file", "err", err)
	}
}

func closeRoot(root *os.Root) {
	if err := root.Close(); err != nil {
		slog.Warn("close drive local root", "err", err)
	}
}

func closeLocal(file *os.File) {
	if err := file.Close(); err != nil {
		slog.Warn("close drive local file", "err", err)
	}
}
