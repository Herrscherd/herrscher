package tui

import "os/exec"

func bestEffort(_ error) {}

func reap(cmd *exec.Cmd) {
	bestEffort(cmd.Wait())
}
