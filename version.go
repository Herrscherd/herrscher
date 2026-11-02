package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

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
