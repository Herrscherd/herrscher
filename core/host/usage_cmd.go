package host

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

const (
	transcriptIndexFile = "transcripts.json"
	pricesFile          = "prices.json"
	transcriptScanEvery = time.Minute
)

const (
	usageVerbName     = "usage"
	usageParamRange   = "range"
	usageParamAccount = "account"
	usageParamVendor  = "vendor"
)

func usageDir(partDir string) string {
	return filepath.Join(partDir, "usage")
}

func (h *hub) recordUsage(sess state.Session, records []usage.Record) {
	ledger := usage.Ledger{Dir: usageDir(h.partDir), Now: time.Now}
	if err := ledger.Append(stampUsage(records, h.st, sess)); err != nil {
		warnLive(h.warningLanguage(), "warn.session_usage", map[string]any{nameParam: sess.Name, detailParam: err})
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
	ledger      usage.Ledger
	limits      *usage.LimitsCache
	accounts    accounts.Store
	transcripts *usage.TranscriptIndex
	prices      *usage.PriceSource
	mu          sync.Mutex
	reported    map[string]bool
}

func newUsageSource(partDir string) *usageSource {
	store, err := accountStore(BackendRequest{})
	if err != nil {
		slog.Warn("usage: account store unavailable", "err", err)
	}
	return &usageSource{
		ledger:      usage.Ledger{Dir: usageDir(partDir), Now: time.Now},
		limits:      &usage.LimitsCache{Now: time.Now, Timeout: limitsTimeout(os.Getenv)},
		accounts:    store,
		transcripts: &usage.TranscriptIndex{Path: filepath.Join(usageDir(partDir), transcriptIndexFile), Now: time.Now},
		prices:      &usage.PriceSource{Path: filepath.Join(usageDir(partDir), pricesFile), URL: usage.PricesURL, Now: time.Now},
	}
}

func (s *usageSource) startWatch(ctx context.Context) func() {
	watchCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watch(watchCtx)
	}()
	return func() {
		stop()
		<-done
	}
}

func (s *usageSource) watch(ctx context.Context) {
	ticker := time.NewTicker(transcriptScanEvery)
	defer ticker.Stop()
	for {
		if err := s.prices.Refresh(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("usage: price list not refreshed", "err", err)
		}
		if err := s.transcripts.Scan(ctx, s.transcriptSources()); err != nil && ctx.Err() == nil {
			slog.Warn("usage: transcripts not read", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *usageSource) transcriptSources() []usage.TranscriptSource {
	var sources []usage.TranscriptSource
	for _, vendor := range []string{contracts.VendorClaude, contracts.VendorCodex} {
		if home := nativeHome(vendor); home != "" {
			sources = append(sources, usage.TranscriptSource{Vendor: vendor, Home: home})
		}
	}
	profiles, err := s.accounts.List()
	if err != nil {
		slog.Warn("usage: account profiles unreadable", "err", err)
	}
	for _, profile := range profiles {
		sources = append(sources, usage.TranscriptSource{Vendor: profile.Vendor, Account: profile.Name, Home: profile.Dir})
	}
	return sources
}

func nativeHome(vendor string) string {
	name, err := contracts.ProfileHomeEnv(vendor)
	if err != nil {
		return ""
	}
	if home := envx.Getenv(name); home != "" {
		return home
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(user, "."+vendor)
}

type usageQuery struct {
	rng     usage.Range
	account usage.AccountKey
	vendor  string
}

func (q usageQuery) matches(vendor, account string) bool {
	return (q.vendor == "" || vendor == q.vendor) && q.account.Matches(vendor, account)
}

func (q usageQuery) keeps(record usage.Record) bool {
	return q.matches(record.Vendor, record.Account)
}

func (s *usageSource) run(ctx context.Context, query usageQuery) usage.Snapshot {
	records, err := s.ledger.Records(query.rng)
	if err != nil {
		slog.Warn("usage ledger not read whole", "err", err)
	}
	now := s.ledger.Now()
	records, notes := s.withTranscripts(records, now.Add(-query.rng.Span()))
	s.priceCompactions(records)
	records = slices.DeleteFunc(records, func(record usage.Record) bool {
		return !query.keeps(record)
	})
	return usage.Snapshot{
		At:                 now,
		Range:              query.rng,
		Account:            query.account,
		Vendor:             query.vendor,
		Limits:             s.limits.Read(ctx, s.targets(query)),
		Spend:              usage.Summarize(records, query.rng, now, query.account),
		LedgerTruncated:    err != nil,
		TranscriptsPending: notes.pending,
		Unreadable:         notes.unreadable,
		PricesMissing:      !notes.priced,
	}
}

func (s *usageSource) priceCompactions(records []usage.Record) {
	var book usage.PriceBook
	if s.prices != nil {
		book = s.prices.Book()
	}
	book.PriceCompactions(records)
}

type transcriptState struct {
	pending    bool
	unreadable int
	priced     bool
}

func (s *usageSource) withTranscripts(ledger []usage.Record, since time.Time) ([]usage.Record, transcriptState) {
	if s.transcripts == nil || s.prices == nil {
		return ledger, transcriptState{priced: true}
	}
	view := s.transcripts.View(since)
	records := slices.DeleteFunc(ledger, func(record usage.Record) bool {
		return record.Compaction == nil && view.Covered[usage.AccountKey{Vendor: record.Vendor, Account: record.Account}]
	})
	book := s.prices.Book()
	book.Price(view.Records)
	return append(records, view.Records...), transcriptState{
		pending:    !view.Scanned,
		unreadable: view.Unreadable,
		priced:     !book.Empty() || len(view.Records) == 0,
	}
}

func (s *usageSource) targets(query usageQuery) []usage.Target {
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
		return !query.matches(target.Vendor, string(target.Account))
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
		Param(usageParamVendor, clitext.CmdHelpUsageVendor, false).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			rng, err := usageRange(input)
			if err != nil {
				return contracts.Result{}, err
			}
			key, err := usageAccount(input)
			if err != nil {
				return contracts.Result{}, err
			}
			vendor, given := input.Lookup(usageParamVendor)
			if given && vendor == "" {
				return contracts.Result{}, usage.ErrEmptyValue
			}
			snapshot := source.run(ctx, usageQuery{rng: rng, account: key, vendor: vendor})
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
