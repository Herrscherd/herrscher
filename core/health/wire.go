package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/compactwire"
)

// ErrMalformed reports a health answer that cannot be decoded.
var ErrMalformed = errors.New("health answer malformed")

type wireSnapshot struct {
	PID      int         `json:"pid"`
	Started  time.Time   `json:"started"`
	Socket   string      `json:"socket"`
	Version  string      `json:"version,omitempty"`
	ProbedAt time.Time   `json:"probed_at"`
	Elapsed  int64       `json:"elapsed_ns"`
	Checks   []wireCheck `json:"checks"`
	Stack    []string    `json:"compact_stack,omitempty"`
}

type wireCheck struct {
	Group      Group                 `json:"group"`
	Name       string                `json:"name"`
	Account    string                `json:"account,omitempty"`
	Default    bool                  `json:"default,omitempty"`
	State      contracts.HealthState `json:"state"`
	Latency    int64                 `json:"latency_ns,omitempty"`
	Detail     string                `json:"detail,omitempty"`
	Remedy     string                `json:"remedy,omitempty"`
	Socket     string                `json:"socket,omitempty"`
	Listen     string                `json:"listen,omitempty"`
	Providers  int                   `json:"providers,omitempty"`
	Pool       *wirePool             `json:"pool,omitempty"`
	TimedOut   int64                 `json:"timed_out_ns,omitempty"`
	Compaction *wireCompaction       `json:"compaction,omitempty"`
}

type wireCompaction struct {
	At      time.Time                `json:"at"`
	Outcome contracts.CompactOutcome `json:"outcome"`
}

type wirePool struct {
	Keys       int   `json:"keys"`
	Usable     int   `json:"usable"`
	LastStatus int   `json:"last_status,omitempty"`
	CoolingFor int64 `json:"cooling_for_ns,omitempty"`
}

// Encode writes snapshot in its wire form. A report without a state goes out
// down, and every notice goes out in the compaction notice encoding, so its
// arguments survive the trip.
func Encode(snapshot Snapshot) ([]byte, error) {
	wire := wireSnapshot{
		PID:      snapshot.Daemon.PID,
		Started:  snapshot.Daemon.Started,
		Socket:   snapshot.Daemon.Socket,
		Version:  snapshot.Daemon.Version,
		ProbedAt: snapshot.ProbedAt,
		Elapsed:  int64(snapshot.Elapsed),
		Stack:    snapshot.CompactStack,
		Checks:   make([]wireCheck, 0, len(snapshot.Checks)),
	}
	for index := range snapshot.Checks {
		check := snapshot.Checks[index]
		encoded, err := encodeCheck(check)
		if err != nil {
			return nil, err
		}
		wire.Checks = append(wire.Checks, encoded)
	}
	out, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("health snapshot: %w", err)
	}
	return out, nil
}

func encodeCheck(check Check) (wireCheck, error) {
	report := Settled(check.Report)
	detail, err := compactwire.EncodeNotice(report.Detail)
	if err != nil {
		return wireCheck{}, fmt.Errorf("health check %s detail: %w", check.Service(), err)
	}
	remedy, err := compactwire.EncodeNotice(report.Remedy)
	if err != nil {
		return wireCheck{}, fmt.Errorf("health check %s remedy: %w", check.Service(), err)
	}
	wire := wireCheck{
		Group:     check.Group,
		Name:      check.Name,
		Account:   string(check.Account),
		Default:   check.Default,
		State:     report.State,
		Latency:   int64(report.Latency),
		Detail:    detail,
		Remedy:    remedy,
		Socket:    check.Facts.Socket,
		Listen:    check.Facts.Listen,
		Providers: check.Facts.Providers,
		TimedOut:  int64(check.Facts.TimedOut),
	}
	if pool := check.Facts.Pool; pool != nil {
		wire.Pool = &wirePool{Keys: pool.Keys, Usable: pool.Usable, LastStatus: pool.LastStatus, CoolingFor: int64(pool.CoolingFor)}
	}
	if last := check.Facts.Compaction; last != nil {
		wire.Compaction = &wireCompaction{At: last.At, Outcome: last.Outcome}
	}
	return wire, nil
}

// Decode reads a snapshot written by [Encode]. An answer it cannot read returns
// an error matching [ErrMalformed].
func Decode(data []byte) (Snapshot, error) {
	var wire wireSnapshot
	if err := json.Unmarshal(data, &wire); err != nil {
		return Snapshot{}, errors.Join(ErrMalformed, err)
	}
	snapshot := Snapshot{
		Daemon:       Daemon{PID: wire.PID, Started: wire.Started, Socket: wire.Socket, Version: wire.Version},
		ProbedAt:     wire.ProbedAt,
		Elapsed:      time.Duration(wire.Elapsed),
		Checks:       make([]Check, 0, len(wire.Checks)),
		CompactStack: wire.Stack,
	}
	for index := range wire.Checks {
		check, err := decodeCheck(wire.Checks[index])
		if err != nil {
			return Snapshot{}, errors.Join(ErrMalformed, err)
		}
		snapshot.Checks = append(snapshot.Checks, check)
	}
	return snapshot, nil
}

func decodeCheck(wire wireCheck) (Check, error) {
	detail, err := compactwire.DecodeNotice(wire.Detail)
	if err != nil {
		return Check{}, err
	}
	remedy, err := compactwire.DecodeNotice(wire.Remedy)
	if err != nil {
		return Check{}, err
	}
	check := Check{
		Group:   wire.Group,
		Name:    wire.Name,
		Account: contracts.ProfileName(wire.Account),
		Default: wire.Default,
		Report:  contracts.ProbeReport{State: wire.State, Latency: time.Duration(wire.Latency), Detail: detail, Remedy: remedy},
		Facts: Facts{
			Socket:    wire.Socket,
			Listen:    wire.Listen,
			Providers: wire.Providers,
			TimedOut:  time.Duration(wire.TimedOut),
		},
	}
	if pool := wire.Pool; pool != nil {
		check.Facts.Pool = &Pool{Keys: pool.Keys, Usable: pool.Usable, LastStatus: pool.LastStatus, CoolingFor: time.Duration(pool.CoolingFor)}
	}
	if last := wire.Compaction; last != nil {
		check.Facts.Compaction = &Compaction{At: last.At, Outcome: last.Outcome}
	}
	return check, nil
}
