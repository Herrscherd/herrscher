package main

import (
	"runtime/debug"
	"testing"
)

func TestFormatVersionUsesBuildInfoVersion(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.1.22"}}

	if got, want := formatVersion(info), "herrscher version 0.1.22"; got != want {
		t.Fatalf("formatVersion() = %q, want %q", got, want)
	}
}

func TestFormatVersionLabelsDevelopmentBuilds(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}

	if got, want := formatVersion(info), "herrscher version (devel)"; got != want {
		t.Fatalf("formatVersion() = %q, want %q", got, want)
	}
}
