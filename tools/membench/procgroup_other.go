//go:build !unix

package main

import "os/exec"

func killGroupOnCancel(*exec.Cmd) {}
