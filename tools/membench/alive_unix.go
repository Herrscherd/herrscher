//go:build unix

package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const zombieState = 'Z'

var errMalformedStat = errors.New("malformed process stat")

func processAlive(pid int) bool {
	if state, err := processState(pid); err == nil {
		return state != zombieState
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func processState(pid int) (byte, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	stat := string(raw)
	end := strings.LastIndex(stat, ")")
	if end < 0 || end+2 >= len(stat) {
		return 0, errMalformedStat
	}
	return stat[end+2], nil
}
