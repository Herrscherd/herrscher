package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	transport "github.com/Herrscherd/herrscher-transport"
	"github.com/Herrscherd/herrscher/core/clitext"
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
	register  func(*grpc.Server) error
	closeReal func() error
}

func hostPlugin(ctx context.Context, lang i18n.Language, category contracts.Category) (hostedPlugin, error) {
	switch category {
	case contracts.CategoryMemory:
		impl, manifest, err := firstMemory(ctx)
		if err != nil {
			return hostedPlugin{}, err
		}
		return hostedPlugin{
			manifest: manifest,
			register: func(s *grpc.Server) error {
				return registerOnRouter(s, func(r *transport.PluginRouter) error { return transport.RegisterMemorySkeleton(r, impl) })
			},
			closeReal: func() error { return impl.Close() },
		}, nil
	case contracts.CategoryOrchestrator:
		impl, manifest, err := firstOrchestrator(ctx, lang)
		if err != nil {
			return hostedPlugin{}, err
		}
		return hostedPlugin{
			manifest: manifest,
			register: func(s *grpc.Server) error {
				return registerOnRouter(s, func(r *transport.PluginRouter) error { return transport.RegisterOrchestratorSkeleton(r, impl) })
			},
			closeReal: func() error { return impl.Close() },
		}, nil
	case contracts.CategoryBackend:
		impl, manifest, err := firstBackend(ctx, lang)
		if err != nil {
			return hostedPlugin{}, err
		}
		return hostedPlugin{
			manifest:  manifest,
			register:  func(s *grpc.Server) error { transport.RegisterBackendSkeleton(s, impl); return nil },
			closeReal: func() error { return impl.Close() },
		}, nil
	case contracts.CategoryGateway, contracts.CategorySkills:
		return hostedPlugin{}, noSkeletonError(lang, category)
	}
	return hostedPlugin{}, noSkeletonError(lang, category)
}

func noSkeletonError(lang i18n.Language, category contracts.Category) error {
	return renderError(lang, errNoSkeleton, clitext.PluginHostNoSkeleton, clitext.Params{"Category": strconv.Quote(string(category))})
}

func newPluginServer(lang i18n.Language, tlsCfg transport.TLSConfig) (*grpc.Server, error) {
	if !tlsCfg.Enabled() {
		return grpc.NewServer(), nil
	}
	creds, err := tlsCfg.ServerCredentials()
	if err != nil {
		return nil, renderError(lang, err, clitext.PluginHostFailed, clitext.Params{"Err": err.Error()})
	}
	return grpc.NewServer(grpc.Creds(creds)), nil
}

func announceEvery(ctx context.Context, conn *nats.Conn, ann transport.Announcement, key transport.AnnounceKey) {
	ticker := time.NewTicker(announceInterval)
	defer ticker.Stop()
	for {
		if err := transport.Announce(conn, ann, key); err != nil {
			slog.Warn("announce plugin", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runPluginHost(ctx context.Context, args []string) error {
	lang := clitext.ClientLanguage()
	tr := clitext.For(lang)
	flags := flag.NewFlagSet("plugin-host", flag.ContinueOnError)
	category := flags.String("category", "", tr.Text(clitext.PluginHostFlagCategory, nil))
	natsURL := flags.String("nats", nats.DefaultURL, tr.Text(clitext.PluginHostFlagNATS, nil))
	instanceID := flags.String("instance", "", tr.Text(clitext.PluginHostFlagInstance, nil))
	if err := tr.Err(); err != nil {
		return err
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !host.SupportedRemoteCategory(contracts.Category(*category)) {
		return renderError(lang, errUnsupportedCategory, clitext.PluginHostUnsupported, clitext.Params{"Category": strconv.Quote(*category)})
	}

	tlsCfg := tlsConfigFromEnv()
	if err := tlsCfg.Validate(); err != nil {
		return renderError(lang, err, clitext.PluginHostFailed, clitext.Params{"Err": err.Error()})
	}

	host.CaptureAnnounceKey()
	announceKey, err := announceKeyFromEnv()
	if err != nil {
		return renderError(lang, err, clitext.PluginHostFailed, clitext.Params{"Err": err.Error()})
	}

	hosted, err := hostPlugin(ctx, lang, contracts.Category(*category))
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
	server, err := newPluginServer(lang, tlsCfg)
	if err != nil {
		return err
	}
	if regErr := hosted.register(server); regErr != nil {
		return regErr
	}

	conn, err := nats.Connect(*natsURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ann := transport.Announcement{Manifest: hosted.manifest, GrpcAddr: lis.Addr().String(), InstanceID: *instanceID}

	go announceEvery(ctx, conn, ann, announceKey)
	go func() { <-ctx.Done(); server.GracefulStop() }()
	return server.Serve(lis)
}

func firstMemory(ctx context.Context) (contracts.Memory, contracts.Manifest, error) {
	return host.BuildFirstMemoryWithManifest(ctx)
}

func firstOrchestrator(ctx context.Context, lang i18n.Language) (contracts.Orchestrator, contracts.Manifest, error) {
	orchestrators := contracts.Default.Orchestrators()
	for i := range orchestrators {
		plugin := &orchestrators[i]
		if plugin.Orchestrator == nil {
			continue
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
		if err != nil {
			return nil, contracts.Manifest{}, renderError(lang, err, clitext.PluginHostOrchestrator, clitext.Params{"Err": err.Error()})
		}
		orch, err := plugin.Orchestrator(ctx, cfg, nil)
		if err != nil {
			return nil, contracts.Manifest{}, renderError(lang, err, clitext.PluginHostOrchestrator, clitext.Params{"Err": err.Error()})
		}
		return orch, plugin.Manifest, nil
	}
	return nil, contracts.Manifest{}, renderError(lang, errNoOrchestratorPlugin, clitext.PluginHostNoOrchestrator, nil)
}

func firstBackend(ctx context.Context, lang i18n.Language) (contracts.Backend, contracts.Manifest, error) {
	backends := contracts.Default.Backends()
	for i := range backends {
		plugin := &backends[i]
		if plugin.Backend == nil {
			continue
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
		if err != nil {
			return nil, contracts.Manifest{}, renderError(lang, err, clitext.PluginHostBackend, clitext.Params{"Err": err.Error()})
		}
		backend, err := plugin.Backend(ctx, cfg)
		if err != nil {
			return nil, contracts.Manifest{}, renderError(lang, err, clitext.PluginHostBackend, clitext.Params{"Err": err.Error()})
		}
		return backend, plugin.Manifest, nil
	}
	return nil, contracts.Manifest{}, renderError(lang, errNoBackendPlugin, clitext.PluginHostNoBackend, nil)
}

func registerOnRouter(server *grpc.Server, registerSkeleton func(*transport.PluginRouter) error) error {
	router, err := transport.NewPluginRouter(server)
	if err != nil {
		return err
	}
	return registerSkeleton(router)
}
