package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"

	contracts "github.com/Herrscherd/herrscher-contracts"
	transport "github.com/Herrscherd/herrscher-transport"
	"github.com/Herrscherd/herrscher/core/host"
)

var (
	errUnsupportedCategory  = errors.New("unsupported category")
	errNoSkeleton           = errors.New("has no skeleton")
	errNoOrchestratorPlugin = errors.New("no orchestrator plugin registered")
	errNoBackendPlugin      = errors.New("no backend plugin registered")
)

type hostedPlugin struct {
	manifest  contracts.Manifest
	register  func(*grpc.Server)
	closeReal func() error
}

func hostPlugin(ctx context.Context, category contracts.Category) (hostedPlugin, error) {
	switch category {
	case contracts.CategoryMemory:
		real, m, err := firstMemory(ctx)
		if err != nil {
			return hostedPlugin{}, err
		}
		return hostedPlugin{
			manifest:  m,
			register:  func(s *grpc.Server) { transport.RegisterMemorySkeleton(s, real) },
			closeReal: func() error { return real.Close() },
		}, nil
	case contracts.CategoryOrchestrator:
		real, m, err := firstOrchestrator(ctx)
		if err != nil {
			return hostedPlugin{}, err
		}
		return hostedPlugin{
			manifest:  m,
			register:  func(s *grpc.Server) { transport.RegisterOrchestratorSkeleton(s, real) },
			closeReal: func() error { return real.Close() },
		}, nil
	case contracts.CategoryBackend:
		real, m, err := firstBackend(ctx)
		if err != nil {
			return hostedPlugin{}, err
		}
		return hostedPlugin{
			manifest:  m,
			register:  func(s *grpc.Server) { transport.RegisterBackendSkeleton(s, real) },
			closeReal: func() error { return real.Close() },
		}, nil
	default:
		return hostedPlugin{}, fmt.Errorf("plugin-host: category %q passed the support gate but %w", category, errNoSkeleton)
	}
}

func newPluginServer(tlsCfg transport.TLSConfig) (*grpc.Server, error) {
	if !tlsCfg.Enabled() {
		return grpc.NewServer(), nil
	}
	creds, err := tlsCfg.ServerCredentials()
	if err != nil {
		return nil, fmt.Errorf("plugin-host: %w", err)
	}
	return grpc.NewServer(grpc.Creds(creds)), nil
}

func announceEvery(ctx context.Context, nc *nats.Conn, ann transport.Announcement) {
	t := time.NewTicker(announceInterval)
	defer t.Stop()
	for {
		if err := transport.Announce(nc, ann); err != nil {
			slog.Warn("announce plugin", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func runPluginHost(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("plugin-host", flag.ContinueOnError)
	category := fs.String("category", "", "plugin category to host (e.g. memory, orchestrator)")
	natsURL := fs.String("nats", nats.DefaultURL, "NATS server URL")
	instanceID := fs.String("instance", "", "process instance id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !host.SupportedRemoteCategory(contracts.Category(*category)) {
		return fmt.Errorf("plugin-host: %w %q", errUnsupportedCategory, *category)
	}

	tlsCfg := tlsConfigFromEnv()
	if err := tlsCfg.Validate(); err != nil {
		return fmt.Errorf("plugin-host: %w", err)
	}

	hosted, err := hostPlugin(ctx, contracts.Category(*category))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := hosted.closeReal(); closeErr != nil {
			slog.Error("close hosted plugin", "err", closeErr)
		}
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", bindAddr())
	if err != nil {
		return err
	}
	s, err := newPluginServer(tlsCfg)
	if err != nil {
		return err
	}
	hosted.register(s)

	nc, err := nats.Connect(*natsURL)
	if err != nil {
		return err
	}
	defer nc.Close()
	ann := transport.Announcement{Manifest: hosted.manifest, GrpcAddr: lis.Addr().String(), InstanceID: *instanceID}

	go announceEvery(ctx, nc, ann)
	go func() { <-ctx.Done(); s.GracefulStop() }()
	return s.Serve(lis)
}

func firstMemory(ctx context.Context) (contracts.Memory, contracts.Manifest, error) {
	return host.BuildFirstMemoryWithManifest(ctx)
}

func firstOrchestrator(ctx context.Context) (contracts.Orchestrator, contracts.Manifest, error) {
	for _, p := range contracts.Default.Orchestrators() {
		if p.Orchestrator == nil {
			continue
		}
		cfg, err := contracts.Resolve(p.Manifest.Config, os.Getenv)
		if err != nil {
			return nil, contracts.Manifest{}, fmt.Errorf("orchestrator plugin-host: %w", err)
		}
		o, err := p.Orchestrator(ctx, cfg, nil)
		if err != nil {
			return nil, contracts.Manifest{}, fmt.Errorf("orchestrator plugin-host: %w", err)
		}
		return o, p.Manifest, nil
	}
	return nil, contracts.Manifest{}, fmt.Errorf("plugin-host: %w", errNoOrchestratorPlugin)
}

func firstBackend(ctx context.Context) (contracts.Backend, contracts.Manifest, error) {
	for _, p := range contracts.Default.Backends() {
		if p.Backend == nil {
			continue
		}
		cfg, err := contracts.Resolve(p.Manifest.Config, os.Getenv)
		if err != nil {
			return nil, contracts.Manifest{}, fmt.Errorf("backend plugin-host: %w", err)
		}
		b, err := p.Backend(ctx, cfg)
		if err != nil {
			return nil, contracts.Manifest{}, fmt.Errorf("backend plugin-host: %w", err)
		}
		return b, p.Manifest, nil
	}
	return nil, contracts.Manifest{}, fmt.Errorf("plugin-host: %w", errNoBackendPlugin)
}
