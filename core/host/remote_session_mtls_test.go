package host

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"

	contracts "github.com/Herrscherd/herrscher-contracts"
	transport "github.com/Herrscherd/herrscher-transport"
)

const mtlsSessionToken = "tok-mtls-never-leaves-the-daemon"

type mtlsRemoteBackend struct {
	mu      sync.Mutex
	prompts []contracts.Prompt
}

func (b *mtlsRemoteBackend) Respond(ctx context.Context, prompt contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	b.mu.Lock()
	b.prompts = append(b.prompts, prompt)
	b.mu.Unlock()
	asker, asked := contracts.ApprovalAskerFrom(ctx)
	if !asked {
		return "no approval path", nil
	}
	answer := asker(ctx, contracts.ApprovalRequest{ID: "req-1", Tool: "Bash", Subject: "rm -rf build"})
	return "remote saw " + string(answer.Decision) + ": " + answer.Reason, nil
}

func (b *mtlsRemoteBackend) Close() error { return nil }

func (b *mtlsRemoteBackend) received() []contracts.Prompt {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]contracts.Prompt(nil), b.prompts...)
}

func remoteSessionOverMTLS(t *testing.T) (contracts.Backend, *mtlsRemoteBackend) {
	t.Helper()
	dir := genTestCerts(t)
	tlsCfg := transport.TLSConfig{
		CAFile:   filepath.Join(dir, "ca.pem"),
		CertFile: filepath.Join(dir, "leaf.pem"),
		KeyFile:  filepath.Join(dir, "leaf-key.pem"),
	}
	serverCreds, err := tlsCfg.ServerCredentials()
	if err != nil {
		t.Fatalf("server creds: %v", err)
	}
	clientCreds, err := tlsCfg.ClientCredentials()
	if err != nil {
		t.Fatalf("client creds: %v", err)
	}

	srv, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(2 * time.Second) {
		t.Fatal("nats not ready")
	}
	t.Cleanup(srv.Shutdown)

	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	remote := &mtlsRemoteBackend{}
	gs := grpc.NewServer(grpc.Creds(serverCreds))
	transport.RegisterBackendSkeleton(gs, remote)
	go func() { logCleanup(gs.Serve(lis)) }()
	t.Cleanup(gs.Stop)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	key := testAnnounceKey(t)
	ann := transport.Announcement{
		Manifest:   contracts.Manifest{Kind: "fakeremote", Category: contracts.CategoryBackend},
		GrpcAddr:   lis.Addr().String(),
		InstanceID: "backend-0",
	}
	stop := make(chan struct{})
	go func() {
		for {
			logCleanup(transport.Announce(nc, ann, key))
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
	t.Cleanup(func() { close(stop) })

	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "fakeremote", Category: contracts.CategoryBackend},
		Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
			return nil, &UnknownError{Kind: "backend", Name: "fakeremote"}
		},
	})

	resolver := NewResolver(NewMessages(), categorySet(contracts.CategoryBackend), srv.ClientURL())
	resolver.SetAnnounceKey(key)
	resolver.SetCredentials(clientCreds)
	backend, err := BuildBackendFor(context.Background(), NewMessages(), BackendRequest{
		Vendor:       "fakeremote",
		Cmd:          "fakeremote",
		Session:      "s1",
		SessionToken: mtlsSessionToken,
		resolver:     resolver,
	})
	if err != nil {
		t.Fatalf("a session on a remote backend over mTLS must build: %v", err)
	}
	t.Cleanup(func() { logCleanup(backend.Close()) })
	return backend, remote
}

func TestASessionRunsOnARemoteBackendOverMTLS(t *testing.T) {
	backend, remote := remoteSessionOverMTLS(t)
	reply, err := backend.Respond(context.Background(), contracts.Prompt{Content: "hello"}, nil)
	if err != nil {
		t.Fatalf("Respond over mTLS: %v", err)
	}
	if reply != "no approval path" {
		t.Fatalf("reply %q: the turn without an asker must reach the remote backend", reply)
	}
	if got := remote.received(); len(got) != 1 || got[0].Content != "hello" {
		t.Fatalf("remote received %+v, want the one prompt of the turn", got)
	}
}

func TestAnApprovalOnTheRemoteTurnComesBackWithTheDaemonsDecision(t *testing.T) {
	backend, _ := remoteSessionOverMTLS(t)
	var asked []contracts.ApprovalRequest
	asker := func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		asked = append(asked, req)
		return contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionDeny, Reason: "daemon says no"}
	}
	ctx := contracts.WithApprovalAsker(context.Background(), asker)
	reply, err := backend.Respond(ctx, contracts.Prompt{Content: "clean up"}, nil)
	if err != nil {
		t.Fatalf("Respond over mTLS: %v", err)
	}
	if len(asked) != 1 || asked[0].Tool != "Bash" {
		t.Fatalf("the daemon was asked %+v, want the one Bash request of the remote turn", asked)
	}
	if reply != "remote saw deny: daemon says no" {
		t.Fatalf("reply %q: the remote turn must receive the daemon's decision", reply)
	}
}

func TestNoTokenOrLocalOutboxReachesTheRemoteSide(t *testing.T) {
	backend, remote := remoteSessionOverMTLS(t)
	asker := func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		return contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionAllow}
	}
	ctx := contracts.WithApprovalAsker(context.Background(), asker)
	if _, err := backend.Respond(ctx, contracts.Prompt{Content: "x", Outbox: "/srv/op/outbox/t1"}, nil); err != nil {
		t.Fatalf("Respond over mTLS: %v", err)
	}
	got := remote.received()
	if len(got) != 1 {
		t.Fatalf("remote received %d prompts, want 1", len(got))
	}
	if got[0].Outbox != "" {
		t.Fatalf("the remote side received the local outbox %q", got[0].Outbox)
	}
	wire, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal received prompt: %v", err)
	}
	if strings.Contains(string(wire), mtlsSessionToken) {
		t.Fatalf("the session token crossed the network in %s", wire)
	}
}
