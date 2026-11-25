package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const ownerFileName = "owner.pid"

var runDirName = regexp.MustCompile(`^run-\d+$`)

func writeOwner(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, ownerFileName), []byte(strconv.Itoa(os.Getpid())), credentialsMode); err != nil {
		return sandboxError(err)
	}
	return nil
}

func sweepOrphans(benchRoot string) error {
	entries, err := os.ReadDir(benchRoot)
	if err != nil {
		return sweepError(err)
	}
	var failures []error
	for _, entry := range entries {
		if !entry.IsDir() || !runDirName.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(benchRoot, entry.Name())
		if !orphaned(dir) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			failures = append(failures, sweepError(err))
		}
	}
	return errors.Join(failures...)
}

func orphaned(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, ownerFileName))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	return !processAlive(pid)
}
