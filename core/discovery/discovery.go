// Package discovery finds the machines a session could run on: herrscher daemons
// announcing themselves on the local network, online peers of the tailnet and the
// concrete ssh aliases of the user's configuration. Discovery reads and proposes;
// it never trusts a machine it finds and never writes a host: the operator adds
// the ones they choose.
package discovery

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
)

// Route names the network path a candidate was found on.
type Route uint8

const (
	// RouteLAN is a daemon announced by DNS-SD on the local network.
	RouteLAN Route = iota + 1
	// RouteTailnet is a peer of the Tailscale network that is online.
	RouteTailnet
	// RouteSSH is a concrete host alias of the ssh configuration.
	RouteSSH
)

// String returns the lowercase name of the route, as the operator reads it.
func (r Route) String() string {
	switch r {
	case RouteLAN:
		return "lan"
	case RouteTailnet:
		return "tailnet"
	case RouteSSH:
		return "ssh"
	}
	return "unknown"
}

// MarshalText renders the route by its name, so that JSON output reads as words.
func (r Route) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

const fingerprintLength = 16

// ErrFingerprintInvalid reports a fingerprint that is not sixteen lowercase hex digits.
var ErrFingerprintInvalid = errors.New("fingerprint is not sixteen lowercase hex digits")

// Fingerprint is the short digest of a daemon's public key that identifies it.
// The zero value stands for a fingerprint that is not known.
type Fingerprint struct {
	digest string
}

// ParseFingerprint checks the text of a fingerprint and returns it as a value.
func ParseFingerprint(text string) (Fingerprint, error) {
	if !isLowerHex(text) {
		return Fingerprint{}, ErrFingerprintInvalid
	}
	return Fingerprint{digest: text}, nil
}

func isLowerHex(text string) bool {
	if len(text) != fingerprintLength {
		return false
	}
	for index := range len(text) {
		digit := text[index]
		if digit < '0' || digit > '9' && digit < 'a' || digit > 'f' {
			return false
		}
	}
	return true
}

// String returns the fingerprint, or the empty string when it is not known.
func (f Fingerprint) String() string {
	return f.digest
}

// IsZero reports whether the fingerprint is not known.
func (f Fingerprint) IsZero() bool {
	return f.digest == ""
}

// MarshalText renders the fingerprint as its text, empty when it is not known.
func (f Fingerprint) MarshalText() ([]byte, error) {
	return []byte(f.digest), nil
}

// Candidate is a machine that discovery proposes to the operator.
type Candidate struct {
	Name        string      `json:"name"`
	Address     string      `json:"address"`
	Route       Route       `json:"route"`
	Fingerprint Fingerprint `json:"fingerprint"`
	SSHTarget   string      `json:"ssh_target,omitempty"`
}

// Source is one way of finding candidates, kept behind an interface so that the
// network and the files it reads can be replaced by fakes in tests.
type Source interface {
	// Name is the short label of the source, shown when the source is unavailable.
	Name() string
	// Find returns the candidates the source sees now, and stops when ctx ends.
	Find(ctx context.Context) ([]Candidate, error)
}

// Report is the outcome of one discovery: the candidates found, sorted, and the
// names of the sources that could not be read.
type Report struct {
	Candidates  []Candidate `json:"candidates"`
	Unavailable []string    `json:"unavailable,omitempty"`
}

type sourceOutcome struct {
	name  string
	found []Candidate
	err   error
}

// Discover asks every source at once and merges what they return. A source that
// fails is listed as unavailable and does not stop the others.
func Discover(ctx context.Context, sources []Source) Report {
	outcomes := make([]sourceOutcome, len(sources))
	var pending sync.WaitGroup
	for index, source := range sources {
		pending.Go(func() {
			found, err := source.Find(ctx)
			outcomes[index] = sourceOutcome{name: source.Name(), found: found, err: err}
		})
	}
	pending.Wait()

	var report Report
	for _, outcome := range outcomes {
		if outcome.err != nil {
			report.Unavailable = append(report.Unavailable, outcome.name)
			continue
		}
		report.Candidates = append(report.Candidates, outcome.found...)
	}
	slices.SortFunc(report.Candidates, compareCandidates)
	report.Candidates = slices.Compact(report.Candidates)
	return report
}

func compareCandidates(a, b Candidate) int {
	return cmp.Or(
		cmp.Compare(a.Route, b.Route),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.Address, b.Address),
		cmp.Compare(a.Fingerprint.digest, b.Fingerprint.digest),
		cmp.Compare(a.SSHTarget, b.SSHTarget),
	)
}
