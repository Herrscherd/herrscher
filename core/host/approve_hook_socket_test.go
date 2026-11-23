package host

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/approvalsock"
	"github.com/Herrscherd/herrscher-contracts/echotest"
)

func privateSocketPath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sock")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	return filepath.Join(dir, "approval.sock")
}

func listenApprovals(t *testing.T, asker contracts.ApprovalAsker) string {
	t.Helper()
	path := privateSocketPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- approvalsock.Serve(ctx, path, asker, approvalsock.WithErrorHandler(func(error) {}))
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	for range 500 {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("approval socket %s never appeared", path)
	return ""
}

func forbidDaemon(t *testing.T) hookAsker {
	t.Helper()
	return func(context.Context, []string) (string, bool, error) {
		t.Error("the command socket must not be asked when the approval socket is set")
		return "", false, nil
	}
}

const bashPayload = `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git push"}}`

func TestHookAnswersFromTheApprovalSocketWithoutTheDaemon(t *testing.T) {
	pinEnglish(t)
	t.Setenv(SessionVar, "")
	t.Setenv(TokenVar, "")
	asked := make(chan contracts.ApprovalRequest, 1)
	path := listenApprovals(t, func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		asked <- req
		return contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionAllow}
	})
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, forbidDaemon(t))
	req := <-asked
	if req.Tool != "Bash" || req.Subject != "git push" || req.ID == "" {
		t.Fatalf("request %+v, want Bash with subject git push and an ID", req)
	}
	if d, _ := hookDecision(t, out.String()); d != "allow" {
		t.Fatalf("decision %q, want allow", d)
	}
}

func TestHookCarriesTheDenialAndItsReasonFromTheApprovalSocket(t *testing.T) {
	pinEnglish(t)
	path := listenApprovals(t, func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		return contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionDeny, Reason: "herrscher: not now"}
	})
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, forbidDaemon(t))
	d, reason := hookDecision(t, out.String())
	if d != "deny" || reason != "herrscher: not now" {
		t.Fatalf("got %q/%q, want deny/herrscher: not now", d, reason)
	}
}

func TestHookRefusesWhenTheApprovalSocketIsUnreachable(t *testing.T) {
	pinEnglish(t)
	t.Setenv(approvalsock.EnvApprovalSocket, privateSocketPath(t))
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, forbidDaemon(t))
	d, reason := hookDecision(t, out.String())
	if d != "deny" || !strings.Contains(reason, "could not answer") {
		t.Fatalf("got %q/%q, want a refusal saying the approval socket could not answer", d, reason)
	}
	if !strings.Contains(errOut.String(), "could not answer") {
		t.Fatalf("stderr was %q, want the same refusal", errOut.String())
	}
}

func TestHookRefusesAnAnswerThatNamesAnotherRequest(t *testing.T) {
	pinEnglish(t)
	path := privateSocketPath(t)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { closeRead(listener) })
	go answerWith(listener, `{"ID":"someone-else","Decision":"allow"}`)
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, forbidDaemon(t))
	d, reason := hookDecision(t, out.String())
	const want = "herrscher: the approval socket answered another request, so this tool call is refused"
	if d != "deny" || reason != want {
		t.Fatalf("got %q/%q, want deny/%q for the answer that names another request", d, reason, want)
	}
}

func TestHookRefusesAnApprovalAnswerWithAnUnknownDecision(t *testing.T) {
	pinEnglish(t)
	path := privateSocketPath(t)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { closeRead(listener) })
	go answerRequestWith(listener, func(id string) string {
		return `{"ID":"` + id + `","Decision":"maybe"}`
	})
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, forbidDaemon(t))
	d, reason := hookDecision(t, out.String())
	const want = "herrscher: the approval socket sent an unknown decision, so this tool call is refused"
	if d != "deny" || reason != want {
		t.Fatalf("got %q/%q, want deny/%q", d, reason, want)
	}
}

func TestHookRefusesAnApprovalRequestOverTheSocketBound(t *testing.T) {
	pinEnglish(t)
	t.Setenv(approvalsock.EnvApprovalSocket, privateSocketPath(t))
	oversized := `{"hook_event_name":"PreToolUse","tool_name":"` + strings.Repeat("a", 70<<10) + `","tool_input":{}}`
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(oversized), &out, &errOut, testTarget, forbidDaemon(t))
	d, reason := hookDecision(t, out.String())
	const want = "herrscher: the approval request is over the size limit, so this tool call is refused"
	if d != "deny" || reason != want {
		t.Fatalf("got %q/%q, want deny/%q", d, reason, want)
	}
}

func TestHookRefusesAnApprovalCancelledBeforeTheSocketAnswers(t *testing.T) {
	pinEnglish(t)
	path := privateSocketPath(t)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { closeRead(listener) })
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	mustRunHook(ctx, t, strings.NewReader(bashPayload), &out, &errOut, testTarget, forbidDaemon(t))
	d, reason := hookDecision(t, out.String())
	const want = "herrscher: the approval was cancelled before an answer came, so this tool call is refused"
	if d != "deny" || reason != want {
		t.Fatalf("got %q/%q, want deny/%q", d, reason, want)
	}
}

func answerRequestWith(listener net.Listener, answer func(id string) string) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer closeRead(conn)
	line, readErr := bufio.NewReader(conn).ReadString('\n')
	if readErr != nil {
		return
	}
	var request contracts.ApprovalRequest
	if json.Unmarshal([]byte(line), &request) != nil {
		return
	}
	_, err = io.WriteString(conn, answer(request.ID)+"\n")
	logCleanup(err)
}

func answerWith(listener net.Listener, answer string) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer closeRead(conn)
	if _, readErr := bufio.NewReader(conn).ReadString('\n'); readErr != nil {
		return
	}
	_, err = io.WriteString(conn, answer+"\n")
	logCleanup(err)
}

func TestHookRefusesAMalformedApprovalAnswerWithoutEchoingIt(t *testing.T) {
	pinEnglish(t)
	path := privateSocketPath(t)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { closeRead(listener) })
	go answerWith(listener, echotest.Probe)
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, forbidDaemon(t))
	d, reason := hookDecision(t, out.String())
	const want = "herrscher: the approval socket could not answer, so this tool call is refused"
	if d != "deny" || reason != want {
		t.Fatalf("got %q/%q, want deny/%q", d, reason, want)
	}
	if got := strings.TrimSuffix(errOut.String(), "\n"); got != want {
		t.Fatalf("stderr = %q, want the fixed message %q", got, want)
	}
	echotest.RequireNoSecretFragment(t, out.String()+errOut.String(), echotest.Probe)
}

func TestHookKeepsTheDaemonPathWhenTheApprovalSocketIsUnset(t *testing.T) {
	pinEnglish(t)
	hookSession(t)
	t.Setenv(approvalsock.EnvApprovalSocket, "")
	asked := false
	ask := func(context.Context, []string) (string, bool, error) {
		asked = true
		return `{"decision":"allow"}`, true, nil
	}
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, ask)
	if !asked {
		t.Fatal("the daemon was never asked, so the empty variable turned the command path off")
	}
}
