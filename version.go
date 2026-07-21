package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// formatVersion renders the stable machine-readable version line consumed by
// Neublox. A Go-installed binary carries its module version in BuildInfo;
// binaries built directly from a checkout truthfully identify themselves as
// development builds.
func formatVersion(info *debug.BuildInfo) string {
	version := "(unknown)"
	if info != nil && info.Main.Version != "" {
		version = info.Main.Version
	}
	version = strings.TrimPrefix(version, "v")
	return fmt.Sprintf("herrscher version %s", version)
}

func herrscherVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return formatVersion(nil)
	}
	return formatVersion(info)
}
