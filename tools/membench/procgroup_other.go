//go:build !unix

package main

import "os/exec"

func killGroupOnCancel(*exec.Cmd) {}

func reapGroup(*exec.Cmd) error { return nil }
