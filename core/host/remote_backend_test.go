package host

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type remoteRecorder struct {
	prompts []contracts.Prompt
	plain   int
	relayed int
}

func (r *remoteRecorder) Respond(_ context.Context, prompt contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	r.plain++
	r.prompts = append(r.prompts, prompt)
	return "plain", nil
}

func (r *remoteRecorder) RespondWithApprovals(_ context.Context, prompt contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	r.relayed++
	r.prompts = append(r.prompts, prompt)
	return "relayed", nil
}

func (r *remoteRecorder) Close() error { return nil }

type backendResolver struct {
	backend contracts.Backend
}

func (f *backendResolver) Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, bool, error) {
	return f.backend, true, nil
}

func TestASessionOnARemoteBackendIsAcceptedInsteadOfRefused(t *testing.T) {
	rec := &remoteRecorder{}
	backend, err := BuildBackendFor(context.Background(), NewMessages(), BackendRequest{
		Vendor:       "claude",
		Cmd:          "claude",
		Session:      "first",
		SessionToken: "tok-first",
		resolver:     &backendResolver{backend: rec},
	})
	if err != nil {
		t.Fatalf("a session on a remote backend must run, got %v", err)
	}
	if backend == nil {
		t.Fatal("no backend returned for the session")
	}
}

func TestARemoteBackendTakesNoOutboxDirectory(t *testing.T) {
	rec := &remoteRecorder{}
	backend, err := BuildBackendFor(context.Background(), NewMessages(), BackendRequest{
		Vendor:   "claude",
		Cmd:      "claude",
		resolver: &backendResolver{backend: rec},
	})
	if err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if _, err := backend.Respond(context.Background(), contracts.Prompt{Content: "hi", Outbox: "/srv/op/outbox/turn-1"}, nil); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if len(rec.prompts) != 1 {
		t.Fatalf("remote backend got %d prompts, want 1", len(rec.prompts))
	}
	if got := rec.prompts[0].Outbox; got != "" {
		t.Fatalf("remote backend received the local outbox %q, want none", got)
	}
}

func TestARemoteBackendRelaysApprovalsWhenTheTurnHasAnAsker(t *testing.T) {
	rec := &remoteRecorder{}
	backend, err := BuildBackendFor(context.Background(), NewMessages(), BackendRequest{
		Vendor:   "claude",
		Cmd:      "claude",
		resolver: &backendResolver{backend: rec},
	})
	if err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	asker := func(context.Context, contracts.ApprovalRequest) contracts.ApprovalAnswer {
		return contracts.ApprovalAnswer{Decision: contracts.DecisionDeny}
	}
	reply, err := backend.Respond(contracts.WithApprovalAsker(context.Background(), asker), contracts.Prompt{Content: "hi"}, nil)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if rec.relayed != 1 || rec.plain != 0 {
		t.Fatalf("relayed %d, plain %d: the approvals must travel on the stream", rec.relayed, rec.plain)
	}
	if reply != "relayed" {
		t.Fatalf("reply %q, want the relayed one", reply)
	}
}

func TestARemoteBackendWithoutAnAskerTakesThePlainPath(t *testing.T) {
	rec := &remoteRecorder{}
	backend, err := BuildBackendFor(context.Background(), NewMessages(), BackendRequest{
		Vendor:   "claude",
		Cmd:      "claude",
		resolver: &backendResolver{backend: rec},
	})
	if err != nil {
		t.Fatalf("BuildBackendFor: %v", err)
	}
	if _, err := backend.Respond(context.Background(), contracts.Prompt{Content: "hi"}, nil); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if rec.plain != 1 || rec.relayed != 0 {
		t.Fatalf("plain %d, relayed %d: a turn with no asker opens no approval stream", rec.plain, rec.relayed)
	}
}
