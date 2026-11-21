package host

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	contracts "github.com/Herrscherd/herrscher-contracts"
	transport "github.com/Herrscherd/herrscher-transport"

	"github.com/Herrscherd/herrscher/core/internal/lease"
)

const mtlsSessionToken = "tok-mtls-never-leaves-the-daemon"

type mtlsRemoteBackend struct {
	mu        sync.Mutex
	prompts   []contracts.Prompt
	decisions []contracts.ApprovalAnswer
	drive     func(context.Context, contracts.ApprovalAsker) string
}

func (b *mtlsRemoteBackend) Respond(ctx context.Context, prompt contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	b.mu.Lock()
	b.prompts = append(b.prompts, prompt)
	b.mu.Unlock()
	asker, asked := contracts.ApprovalAskerFrom(ctx)
	if !asked {
		return "no approval path", nil
	}
	if drive := b.driver(); drive != nil {
		return drive(ctx, asker), nil
	}
	answer := asker(ctx, contracts.ApprovalRequest{ID: "req-1", Tool: "Bash", Subject: "rm -rf build"})
	b.mu.Lock()
	b.decisions = append(b.decisions, answer)
	b.mu.Unlock()
	return "remote saw " + string(answer.Decision) + ": " + answer.Reason, nil
}

func (b *mtlsRemoteBackend) Close() error { return nil }

func (b *mtlsRemoteBackend) setDriver(drive func(context.Context, contracts.ApprovalAsker) string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drive = drive
}

func (b *mtlsRemoteBackend) driver() func(context.Context, contracts.ApprovalAsker) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.drive
}

func (b *mtlsRemoteBackend) received() []contracts.Prompt {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]contracts.Prompt(nil), b.prompts...)
}

func (b *mtlsRemoteBackend) answered() []contracts.ApprovalAnswer {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]contracts.ApprovalAnswer(nil), b.decisions...)
}

type mtlsRemote struct {
	dir     string
	natsURL string
	key     transport.AnnounceKey
	remote  *mtlsRemoteBackend
}

func startMTLSRemote(t *testing.T) mtlsRemote {
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

	return mtlsRemote{dir: dir, natsURL: srv.ClientURL(), key: key, remote: remote}
}

func (r mtlsRemote) build(t *testing.T, creds credentials.TransportCredentials) (contracts.Backend, error) {
	t.Helper()
	resolver := NewResolver(NewMessages(), categorySet(contracts.CategoryBackend), r.natsURL)
	resolver.SetAnnounceKey(r.key)
	resolver.SetCredentials(creds)
	backend, err := BuildBackendFor(context.Background(), NewMessages(), BackendRequest{
		Vendor:       "fakeremote",
		Cmd:          "fakeremote",
		Session:      "s1",
		SessionToken: mtlsSessionToken,
		resolver:     resolver,
	})
	if err == nil {
		t.Cleanup(func() { logCleanup(backend.Close()) })
	}
	return backend, err
}

func (r mtlsRemote) validClient(t *testing.T) credentials.TransportCredentials {
	t.Helper()
	return mustClientCreds(t, transport.TLSConfig{
		CAFile:   filepath.Join(r.dir, "ca.pem"),
		CertFile: filepath.Join(r.dir, "leaf.pem"),
		KeyFile:  filepath.Join(r.dir, "leaf-key.pem"),
	})
}

func mustClientCreds(t *testing.T, cfg transport.TLSConfig) credentials.TransportCredentials {
	t.Helper()
	creds, err := cfg.ClientCredentials()
	if err != nil {
		t.Fatalf("client creds: %v", err)
	}
	return creds
}

func remoteSessionOverMTLS(t *testing.T) (contracts.Backend, *mtlsRemoteBackend) {
	t.Helper()
	fixture := startMTLSRemote(t)
	backend, err := fixture.build(t, fixture.validClient(t))
	if err != nil {
		t.Fatalf("a session on a remote backend over mTLS must build: %v", err)
	}
	return backend, fixture.remote
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

func TestAClientWithoutACertificateIsRefusedByTheRemoteBackend(t *testing.T) {
	fixture := startMTLSRemote(t)
	pool := x509.NewCertPool()
	caPEM, err := os.ReadFile(filepath.Join(fixture.dir, "ca.pem"))
	if err != nil {
		t.Fatalf("read ca: %v", err)
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("the test CA did not parse")
	}
	anonymous := credentials.NewTLS(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
	backend, err := fixture.build(t, anonymous)
	if err != nil {
		t.Fatalf("building the backend must not dial the remote: %v", err)
	}
	assertRemoteRefusesTurn(t, backend, fixture.remote)
}

func TestAClientCertificateFromAnotherCAIsRefusedByTheRemoteBackend(t *testing.T) {
	fixture := startMTLSRemote(t)
	foreign := genTestCerts(t)
	foreignClient := mustClientCreds(t, transport.TLSConfig{
		CAFile:   filepath.Join(fixture.dir, "ca.pem"),
		CertFile: filepath.Join(foreign, "leaf.pem"),
		KeyFile:  filepath.Join(foreign, "leaf-key.pem"),
	})
	backend, err := fixture.build(t, foreignClient)
	if err != nil {
		t.Fatalf("building the backend must not dial the remote: %v", err)
	}
	assertRemoteRefusesTurn(t, backend, fixture.remote)
}

func assertRemoteRefusesTurn(t *testing.T, backend contracts.Backend, remote *mtlsRemoteBackend) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply, err := backend.Respond(ctx, contracts.Prompt{Content: "hello"}, nil)
	if err == nil {
		t.Fatalf("an unauthenticated client got the reply %q from the remote backend", reply)
	}
	if got := remote.received(); len(got) != 0 {
		t.Fatalf("the remote backend received %d prompts from an unauthenticated client", len(got))
	}
}

func TestTheTransportProxyDeniesAnApprovalWithoutAnAsker(t *testing.T) {
	backend, remote := remoteSessionOverMTLS(t)
	relay, ok := lease.Unwrap(backend).(remoteBackend).Backend.(approvalRelayingBackend)
	if !ok {
		t.Fatal("the remote session's backend must relay approvals through the transport proxy")
	}
	reply, err := relay.RespondWithApprovals(context.Background(), contracts.Prompt{Content: "clean up"}, nil)
	if err != nil {
		t.Fatalf("RespondWithApprovals over mTLS: %v", err)
	}
	answers := remote.answered()
	if len(answers) != 1 {
		t.Fatalf("the remote backend got %d answers, want the one denial of its approval", len(answers))
	}
	if answers[0].Decision != contracts.DecisionDeny || answers[0].Allowed() {
		t.Fatalf("an approval on a turn without an asker was answered %+v, want a denial", answers[0])
	}
	if reply != "remote saw deny: " {
		t.Fatalf("reply %q: the remote turn must receive the transport's denial", reply)
	}
}

func implements[T any](value any) bool {
	_, ok := value.(T)
	return ok
}

func TestTheRemoteDecoratorMasksNoOptionalBackendInterface(t *testing.T) {
	fixture := startMTLSRemote(t)
	backend, err := fixture.build(t, fixture.validClient(t))
	if err != nil {
		t.Fatalf("a session on a remote backend over mTLS must build: %v", err)
	}
	decorated, ok := lease.Unwrap(backend).(remoteBackend)
	if !ok {
		t.Fatalf("a remote session is %T, want remoteBackend", backend)
	}
	proxy := decorated.Backend
	masked := []struct {
		name        string
		implemented bool
	}{
		{"ResumeAware", implements[contracts.ResumeAware](proxy)},
		{"ChoiceAware", implements[contracts.ChoiceAware](proxy)},
		{"ChoiceInjector", implements[contracts.ChoiceInjector](proxy)},
		{"FileProducer", implements[contracts.FileProducer](proxy)},
		{"Compactor", implements[contracts.Compactor](proxy)},
		{"StageTrigger", implements[contracts.StageTrigger](proxy)},
		{"HistoryEditor", implements[contracts.HistoryEditor](proxy)},
		{"SkillNative", implements[contracts.SkillNative](proxy)},
		{"Liveness", implements[contracts.Liveness](proxy)},
	}
	for _, iface := range masked {
		if iface.implemented {
			t.Fatalf("the remote backend implements %s, which remoteBackend masks: relay it in the decorator", iface.name)
		}
	}
}
