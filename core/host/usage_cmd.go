package host

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

const (
	usageVerbName     = "usage"
	usageParamRange   = "range"
	usageParamAccount = "account"
)

func usageDir(partDir string) string {
	return filepath.Join(partDir, "usage")
}

func (h *hub) recordUsage(sess state.Session, records []usage.Record) {
	ledger := usage.Ledger{Dir: usageDir(h.partDir), Now: time.Now}
	if err := ledger.Append(stampUsage(records, h.st, sess)); err != nil {
		warnStderr("herrscher serve: session %q: usage not recorded: %v\n", sess.Name, err)
	}
}

func stampUsage(records []usage.Record, current *state.State, sess state.Session) []usage.Record {
	vendor, account := sess.Vendor, sess.Account
	if live, ok := current.FindSession(sess.Name); ok {
		vendor, account = live.Vendor, live.Account
	}
	for index := range records {
		records[index].Vendor = vendor
		records[index].Account = account
	}
	return records
}

type usageSource struct {
	ledger   usage.Ledger
	limits   *usage.LimitsCache
	accounts accounts.Store
	mu       sync.Mutex
	reported map[string]bool
}

func newUsageSource(partDir string) *usageSource {
	store, err := accountStore(BackendRequest{})
	if err != nil {
		slog.Warn("usage: account store unavailable", "err", err)
	}
	return &usageSource{
		ledger:   usage.Ledger{Dir: usageDir(partDir), Now: time.Now},
		limits:   &usage.LimitsCache{Now: time.Now},
		accounts: store,
	}
}

func (s *usageSource) run(ctx context.Context, rng usage.Range, key usage.AccountKey) (usage.Snapshot, error) {
	records, err := s.ledger.Records(rng)
	truncated := errors.Is(err, usage.ErrLedgerTruncated)
	if err != nil && !truncated {
		return usage.Snapshot{}, err
	}
	now := s.ledger.Now()
	return usage.Snapshot{
		At:              now,
		Range:           rng,
		Account:         key,
		Limits:          s.limits.Read(ctx, s.targets(key)),
		Spend:           usage.Summarize(records, rng, now, key),
		LedgerTruncated: truncated,
	}, nil
}

func (s *usageSource) targets(key usage.AccountKey) []usage.Target {
	profiles, err := s.accounts.List()
	if err != nil {
		slog.Warn("usage: account profiles unreadable", "err", err)
	}
	var targets []usage.Target
	backends := contracts.Default.Backends()
	for index := range backends {
		plugin := backends[index]
		if plugin.Limits == nil {
			continue
		}
		kind := plugin.Manifest.Kind
		targets = append(targets, limitTarget(plugin, "", false, nil))
		for _, profile := range profiles {
			if profile.Vendor != kind {
				continue
			}
			env, envErr := contracts.ProfileEnv(kind, profile.Dir)
			if envErr != nil {
				s.reportProfileEnv(kind, profile.Name, envErr)
				continue
			}
			targets = append(targets, limitTarget(plugin, contracts.ProfileName(profile.Name), profile.Default, env))
		}
	}
	return slices.DeleteFunc(targets, func(target usage.Target) bool {
		return !key.Matches(target.Vendor, string(target.Account))
	})
}

func (s *usageSource) reportProfileEnv(vendor, profile string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := vendor + ":" + profile
	if s.reported[key] {
		return
	}
	if s.reported == nil {
		s.reported = make(map[string]bool)
	}
	s.reported[key] = true
	slog.Warn("usage: profile env unavailable", "vendor", vendor, "profile", profile, "err", err)
}

func limitTarget(plugin contracts.Plugin, account contracts.ProfileName, isDefault bool, env map[string]string) usage.Target {
	getenv := envGetter(env)
	return usage.Target{
		Vendor:  plugin.Manifest.Kind,
		Account: account,
		Default: isDefault,
		Read:    limitReader(plugin.Manifest.Config, plugin.Limits, getenv),
		Access:  contracts.ProbeAccess{Getenv: getenv, Account: account},
	}
}

func limitReader(settings []contracts.Setting, read contracts.LimitReader, getenv func(string) string) contracts.LimitReader {
	return func(ctx context.Context, access contracts.ProbeAccess) ([]contracts.Limit, error) {
		cfg, err := contracts.Resolve(settings, getenv)
		if err != nil {
			return nil, err
		}
		access.Config = cfg
		return read(ctx, access)
	}
}

func usageVerb(source *usageSource) contracts.Cmd {
	return contracts.New(usageVerbName).
		Help(clitext.CmdHelpUsage).
		Param(usageParamRange, clitext.CmdHelpUsageRange, false).
		Param(usageParamAccount, clitext.CmdHelpUsageAccount, false).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			rng, err := usageRange(input)
			if err != nil {
				return contracts.Result{}, err
			}
			key, err := usageAccount(input)
			if err != nil {
				return contracts.Result{}, err
			}
			snapshot, err := source.run(ctx, rng, key)
			if err != nil {
				return contracts.Result{}, err
			}
			raw, encodeErr := usage.Encode(snapshot)
			return cli.Data(string(raw), encodeErr)
		})
}

func usageRange(input contracts.Input) (usage.Range, error) {
	text, given := input.Lookup(usageParamRange)
	if !given {
		return usage.DefaultRange(), nil
	}
	if text == "" {
		return 0, usage.ErrEmptyValue
	}
	return usage.ParseRange(text)
}

func usageAccount(input contracts.Input) (usage.AccountKey, error) {
	text, given := input.Lookup(usageParamAccount)
	if given && text == "" {
		return usage.AccountKey{}, usage.ErrEmptyValue
	}
	return usage.ParseAccountKey(text)
}
