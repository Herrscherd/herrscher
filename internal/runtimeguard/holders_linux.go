//go:build linux

package runtimeguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	procRoot         = "/proc"
	socketLinkPrefix = "socket:["
	socketLinkSuffix = "]"
	statParentIndex  = 1
	lockPIDIndex     = 4
	lockDeviceIndex  = 5
	lockFieldCount   = 6
	lockKeyParts     = 3
)

type fileKey struct {
	major uint64
	minor uint64
	ino   uint64
}

func platformOwners(dir string, entries map[string]fs.FileInfo) (owners, error) {
	foreign, err := foreignProcesses()
	if err != nil {
		return owners{}, err
	}
	sockets, err := socketsHeldBy(dir, foreign)
	if err != nil {
		return owners{}, err
	}
	locks, err := locksHeldBy(entries, foreign)
	if err != nil {
		return owners{}, err
	}
	return owners{known: true, sockets: sockets, locks: locks}, nil
}

func foreignProcesses() (map[int]bool, error) {
	parents, err := parentsByPID()
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	foreign := make(map[int]bool, len(parents))
	for pid := range parents {
		if pid != self && !descends(pid, self, parents) {
			foreign[pid] = true
		}
	}
	return foreign, nil
}

func parentsByPID() (map[int]int, error) {
	procs, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	parents := make(map[int]int)
	for _, proc := range procs {
		pid, pidErr := strconv.Atoi(proc.Name())
		if pidErr != nil {
			continue
		}
		stat, statErr := os.ReadFile(filepath.Join(procRoot, proc.Name(), "stat"))
		if statErr != nil {
			continue
		}
		if ppid, ok := parentOf(string(stat)); ok {
			parents[pid] = ppid
		}
	}
	return parents, nil
}

func parentOf(stat string) (int, bool) {
	closing := strings.LastIndexByte(stat, ')')
	if closing < 0 {
		return 0, false
	}
	fields := strings.Fields(stat[closing+1:])
	if len(fields) <= statParentIndex {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[statParentIndex])
	return ppid, err == nil
}

func descends(pid, ancestor int, parents map[int]int) bool {
	for range len(parents) {
		parent, known := parents[pid]
		if !known || parent <= 0 {
			return false
		}
		if parent == ancestor {
			return true
		}
		pid = parent
	}
	return false
}

func socketsHeldBy(dir string, foreign map[int]bool) (map[string]bool, error) {
	bound, err := boundSocketInodes(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	held := heldSocketInodes(foreign)
	names := make(map[string]bool)
	for inode, name := range bound {
		if held[inode] {
			names[name] = true
		}
	}
	return names, nil
}

func boundSocketInodes(dir string) (map[uint64]string, error) {
	table, err := os.ReadFile(procRoot + "/net/unix")
	if err != nil {
		return nil, err
	}
	bound := make(map[uint64]string)
	for _, line := range strings.Split(string(table), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || filepath.Dir(fields[7]) != dir {
			continue
		}
		inode, parseErr := strconv.ParseUint(fields[6], 10, 64)
		if parseErr == nil {
			bound[inode] = filepath.Base(fields[7])
		}
	}
	return bound, nil
}

func heldSocketInodes(foreign map[int]bool) map[uint64]bool {
	held := make(map[uint64]bool)
	for pid := range foreign {
		addHeldSockets(held, filepath.Join(procRoot, strconv.Itoa(pid)))
	}
	return held
}

func addHeldSockets(held map[uint64]bool, procDir string) {
	fds, err := os.ReadDir(filepath.Join(procDir, "fd"))
	if err != nil {
		return
	}
	for _, fd := range fds {
		link, linkErr := os.Readlink(filepath.Join(procDir, "fd", fd.Name()))
		if linkErr != nil {
			continue
		}
		if inode, ok := socketInode(link); ok {
			held[inode] = true
		}
	}
}

func socketInode(link string) (uint64, bool) {
	digits, hasPrefix := strings.CutPrefix(link, socketLinkPrefix)
	if !hasPrefix {
		return 0, false
	}
	digits, hasSuffix := strings.CutSuffix(digits, socketLinkSuffix)
	if !hasSuffix {
		return 0, false
	}
	inode, err := strconv.ParseUint(digits, 10, 64)
	return inode, err == nil
}

func locksHeldBy(entries map[string]fs.FileInfo, foreign map[int]bool) (map[string]bool, error) {
	table, err := os.ReadFile(procRoot + "/locks")
	if err != nil {
		return nil, err
	}
	held := heldFileKeys(string(table), foreign)
	names := make(map[string]bool)
	for name, info := range entries {
		if !isInstanceLock(name) {
			continue
		}
		if key, ok := keyOf(info); ok && held[key] {
			names[name] = true
		}
	}
	return names, nil
}

func heldFileKeys(table string, foreign map[int]bool) map[fileKey]bool {
	held := make(map[fileKey]bool)
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < lockFieldCount {
			continue
		}
		pid, err := strconv.Atoi(fields[lockPIDIndex])
		if err != nil || !foreign[pid] {
			continue
		}
		if key, ok := parseLockKey(fields[lockDeviceIndex]); ok {
			held[key] = true
		}
	}
	return held
}

func parseLockKey(device string) (fileKey, bool) {
	parts := strings.Split(device, ":")
	if len(parts) != lockKeyParts {
		return fileKey{}, false
	}
	major, majorErr := strconv.ParseUint(parts[0], 16, 64)
	minor, minorErr := strconv.ParseUint(parts[1], 16, 64)
	ino, inoErr := strconv.ParseUint(parts[2], 10, 64)
	if majorErr != nil || minorErr != nil || inoErr != nil {
		return fileKey{}, false
	}
	return fileKey{major: major, minor: minor, ino: ino}, true
}

func keyOf(info fs.FileInfo) (fileKey, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileKey{}, false
	}
	return fileKey{
		major: uint64(unix.Major(stat.Dev)),
		minor: uint64(unix.Minor(stat.Dev)),
		ino:   stat.Ino,
	}, true
}
