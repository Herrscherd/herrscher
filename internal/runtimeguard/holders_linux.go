//go:build linux

package runtimeguard

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	procRoot = "/proc"

	daemonSockets    = true
	holdersKnown     = true
	socketLinkPrefix = "socket:["
	socketLinkSuffix = "]"
)

func socketHolders(dir string) (map[string]bool, error) {
	bound, err := boundSocketInodes(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	held := heldSocketInodes()
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

func heldSocketInodes() map[uint64]bool {
	held := make(map[uint64]bool)
	procs, err := os.ReadDir(procRoot)
	if err != nil {
		return held
	}
	self := strconv.Itoa(os.Getpid())
	for _, proc := range procs {
		if proc.Name() == self || !isPID(proc.Name()) {
			continue
		}
		addHeldSockets(held, filepath.Join(procRoot, proc.Name()))
	}
	return held
}

func isPID(name string) bool {
	_, err := strconv.Atoi(name)
	return err == nil
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
