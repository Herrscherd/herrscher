package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/approvalsock"
	"github.com/Herrscherd/herrscher-contracts/echotest"
)

func TestHookRefusesADaemonAnswerWithoutEchoingIt(t *testing.T) {
	pinEnglish(t)
	hookSession(t)
	t.Setenv(approvalsock.EnvApprovalSocket, "")
	ask := func(context.Context, []string) (string, bool, error) {
		return echotest.Probe, true, nil
	}
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, ask)
	d, reason := hookDecision(t, out.String())
	const want = "herrscher: could not read the daemon's answer, so this tool call is refused"
	if d != "deny" || reason != want {
		t.Fatalf("got %q/%q, want deny/%q", d, reason, want)
	}
	echotest.RequireNoSecretFragment(t, out.String()+errOut.String(), echotest.Probe)
}

func TestHookRefusesADaemonErrorWithoutEchoingIt(t *testing.T) {
	pinEnglish(t)
	hookSession(t)
	t.Setenv(approvalsock.EnvApprovalSocket, "")
	ask := func(context.Context, []string) (string, bool, error) {
		return "", true, fmt.Errorf("dispatch failed on %s", echotest.Probe)
	}
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, ask)
	d, reason := hookDecision(t, out.String())
	const want = "herrscher: the daemon could not answer, so this tool call is refused"
	if d != "deny" || reason != want {
		t.Fatalf("got %q/%q, want deny/%q", d, reason, want)
	}
	echotest.RequireNoSecretFragment(t, out.String()+errOut.String(), echotest.Probe)
}

func TestHookRefusesAPayloadThatIsNotJSONWithoutEchoingIt(t *testing.T) {
	pinEnglish(t)
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(echotest.Probe), &out, &errOut, testTarget, forbidDaemon(t))
	d, reason := hookDecision(t, out.String())
	const want = "herrscher: could not read the hook payload, so this tool call is refused"
	if d != "deny" || reason != want {
		t.Fatalf("got %q/%q, want deny/%q", d, reason, want)
	}
	if got := strings.TrimSuffix(errOut.String(), "\n"); got != want {
		t.Fatalf("stderr = %q, want the fixed message %q", got, want)
	}
	echotest.RequireNoSecretFragment(t, out.String()+errOut.String(), echotest.Probe)
}

func TestApprovalSocketFailureNamesTheCausesWithoutAnExchange(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"foreign peer", approvalsock.ErrForeignPeer, hookForeignPeer},
		{"busy", approvalsock.ErrBusy, hookApprovalBusy},
		{"unclassified", errors.New("dial approval socket"), hookApprovalSocketFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := approvalSocketFailureKey(fmt.Errorf("ask: %w", tc.err)); got != tc.want {
				t.Fatalf("key = %q, want %q", got, tc.want)
			}
		})
	}
}
