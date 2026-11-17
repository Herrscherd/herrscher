//go:build e2e && linux

package e2e_test

import (
	"strings"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

func sessionRunning(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("sessionrunning cannot be negated: it checks the session process before a switch")
	}
	if len(args) != 2 {
		ts.Fatalf("usage: sessionrunning <session> <vendor command>")
	}
	if len(sessionProcesses(ts, args[0], args[1])) == 0 {
		ts.Fatalf("no process of session %s runs %s before the switch, so its stop proves nothing", args[0], args[1])
	}
}

func sessionStopped(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("sessionstopped cannot be negated: it waits for the session process to exit")
	}
	if len(args) != 2 {
		ts.Fatalf("usage: sessionstopped <session> <vendor command>")
	}
	deadline := time.Now().Add(descendantWait)
	for time.Now().Before(deadline) {
		if len(sessionProcesses(ts, args[0], args[1])) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	ts.Fatalf("a process of session %s still runs %s %s after the switch", args[0], args[1], descendantWait)
}

func sessionProcesses(ts *testscript.TestScript, session, vendorCommand string) []int {
	pids, err := processesIn(procRoot, instanceEntry(ts.Getenv(instanceEnv)))
	ts.Check(err)
	sessionFlag := " --session " + session + " "
	commandFlag := " --cmd " + vendorCommand + " "
	var matched []int
	for _, pid := range pids {
		line, err := commandLine(procRoot, pid)
		if gone(err) {
			continue
		}
		ts.Check(err)
		padded := " " + line + " "
		if strings.Contains(padded, sessionFlag) && strings.Contains(padded, commandFlag) {
			matched = append(matched, pid)
		}
	}
	return matched
}
