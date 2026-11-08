package host

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

func TestAttachOneShotTapsRecordsTheUsageOfTheSeed(t *testing.T) {
	var recorded []usage.Record
	var owner string
	runtime := oneShotSeedRuntime{recordUsage: func(sess state.Session, records []usage.Record) {
		owner = sess.Name
		recorded = append(recorded, records...)
	}}
	driver := &sessionDriver{}

	attachOneShotTaps(driver, state.Session{Name: "seed-a"}, runtime)
	driver.sink.Usage([]usage.Record{{In: 7, Out: 3, Cost: 0.02}})

	if owner != "seed-a" || len(recorded) != 1 || recorded[0].In != 7 {
		t.Fatalf("one-shot usage not recorded for its session: owner %q, records %+v", owner, recorded)
	}
}

func TestUsageRangeAndAccountRefuseEmptyValues(t *testing.T) {
	if _, err := usageRange(contracts.Input{Args: map[string]string{usageParamRange: ""}}); !errors.Is(err, usage.ErrEmptyValue) {
		t.Fatalf("empty range error = %v, want ErrEmptyValue", err)
	}
	if _, err := usageAccount(contracts.Input{Args: map[string]string{usageParamAccount: ""}}); !errors.Is(err, usage.ErrEmptyValue) {
		t.Fatalf("empty account error = %v, want ErrEmptyValue", err)
	}
	if rng, err := usageRange(contracts.Input{}); err != nil || rng != usage.DefaultRange() {
		t.Fatalf("absent range = %v, %v; want the default range", rng, err)
	}
	if key, err := usageAccount(contracts.Input{}); err != nil || key != (usage.AccountKey{}) {
		t.Fatalf("absent account = %+v, %v; want the zero key", key, err)
	}
}

func TestProfileEnvFailureIsLoggedOncePerProfile(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	source := &usageSource{}
	failure := errors.New("unreadable env")
	source.reportProfileEnv("claude", "work", failure)
	source.reportProfileEnv("claude", "work", failure)
	source.reportProfileEnv("claude", "home", failure)

	if count := strings.Count(logs.String(), "usage: profile env unavailable"); count != 2 {
		t.Fatalf("warnings = %d, want one per profile (2):\n%s", count, logs.String())
	}
}
