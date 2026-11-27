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
	"strings"
	"sync"
	"unicode"
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
// fails or stops answering when SourceDeadline ends is listed as unavailable and
// does not stop the others. Every source receives that deadline as its context and
// must return once the context ends: Discover waits for all of them. Candidates
// that name the same route, the same machine and the same target are listed once;
// the same machine found on two routes stays twice, since each route leads to its
// own command.
func Discover(ctx context.Context, sources []Source) Report {
	ctx, cancel := context.WithTimeout(ctx, SourceDeadline)
	defer cancel()

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
		for _, candidate := range outcome.found {
			candidate.Address = normalizeAddress(candidate.Address)
			report.Candidates = append(report.Candidates, candidate)
		}
	}
	slices.SortFunc(report.Candidates, compareCandidates)
	report.Candidates = slices.Compact(report.Candidates)
	return report
}

func normalizeAddress(address string) string {
	return strings.ToLower(strings.TrimSuffix(address, "."))
}

func compareCandidates(left, right Candidate) int {
	return cmp.Or(
		cmp.Compare(left.Route, right.Route),
		cmp.Compare(left.Name, right.Name),
		cmp.Compare(left.Address, right.Address),
		cmp.Compare(left.Fingerprint.digest, right.Fingerprint.digest),
		cmp.Compare(left.SSHTarget, right.SSHTarget),
	)
}

func plainText(text string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsControl(char) || isBidiControl(char) {
			return -1
		}
		return char
	}, text)
}

func isBidiControl(char rune) bool {
	return char >= 0x202A && char <= 0x202E || char >= 0x2066 && char <= 0x2069
}
