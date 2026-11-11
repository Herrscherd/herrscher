//go:build !linux

package runtimeguard

import "io/fs"

func platformOwners(string, map[string]fs.FileInfo) (owners, error) {
	return owners{}, nil
}
