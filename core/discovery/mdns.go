package discovery

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	// ServiceType is the DNS-SD service a herrscher daemon announces on the local network.
	ServiceType = "_herrscher._tcp"

	serviceDomain    = "local"
	serviceSuffix    = "." + ServiceType + "." + serviceDomain + "."
	entryBufferSize  = 256
	fingerprintField = "fp="
	maxLANCandidates = 256
)

// MDNSSource browses the local network for announced daemons during Window. The
// browse only sends multicast questions: it changes nothing on the network. It keeps
// at most 256 announced daemons and reports the others as an OverflowError.
//
// Find ends when the browse window ends or when the deadline of its context comes,
// whichever is first: the window is shortened to the time left before that deadline.
// Cancelling the context without a deadline does not interrupt a browse that already
// runs, because hashicorp/mdns stops only on its own timer; it closes the sockets, so
// no answer arrives after the cancellation, and the browse still ends within Window.
// A Find started on a context that already ended returns its error at once.
type MDNSSource struct {
	Window time.Duration
}

// Name returns the label of the DNS-SD source.
func (MDNSSource) Name() string {
	return "dns-sd"
}

// Find browses the service for the window and returns one candidate per announced
// daemon that answered. The browse ends on its own: a silent network is an empty list.
// Answers are read while the browse runs; hashicorp/mdns drops an answer when the
// channel is full, so the reader keeps up with it. Past maxLANCandidates kept
// candidates, further announcements are dropped and counted in the OverflowError.
func (source MDNSSource) Find(ctx context.Context) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	window := source.browseWindow(ctx)
	if window <= 0 {
		return nil, context.DeadlineExceeded
	}

	entries := make(chan *mdns.ServiceEntry, entryBufferSize)
	collected := make(chan collection, 1)
	go func() {
		collected <- collectEntries(entries)
	}()
	err := mdns.QueryContext(ctx, &mdns.QueryParam{
		Service: ServiceType,
		Domain:  serviceDomain,
		Timeout: window,
		Entries: entries,
	})
	close(entries)
	result := <-collected
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if result.dropped > 0 {
		return result.found, OverflowError{Dropped: result.dropped}
	}
	return result.found, nil
}

func (source MDNSSource) browseWindow(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return source.Window
	}
	return min(source.Window, time.Until(deadline))
}

type collection struct {
	found   []Candidate
	dropped int
}

func collectEntries(entries <-chan *mdns.ServiceEntry) collection {
	var result collection
	for entry := range entries {
		candidate, ok := candidateFromEntry(entry)
		if !ok {
			continue
		}
		if len(result.found) == maxLANCandidates {
			result.dropped++
			continue
		}
		result.found = append(result.found, candidate)
	}
	return result
}

func candidateFromEntry(entry *mdns.ServiceEntry) (Candidate, bool) {
	address := addressOf(entry)
	if address == "" {
		return Candidate{}, false
	}
	return Candidate{
		Name:        plainText(strings.TrimSuffix(entry.Name, serviceSuffix)),
		Address:     plainText(address),
		Route:       RouteLAN,
		Fingerprint: fingerprintOf(entry.InfoFields),
	}, true
}

func addressOf(entry *mdns.ServiceEntry) string {
	switch {
	case entry.AddrV4 != nil:
		return entry.AddrV4.String()
	case entry.AddrV6 != nil && !entry.AddrV6.Equal(net.IPv6zero):
		return entry.AddrV6.String()
	}
	return strings.TrimSuffix(entry.Host, ".")
}

func fingerprintOf(fields []string) Fingerprint {
	for _, field := range fields {
		text, found := strings.CutPrefix(field, fingerprintField)
		if !found {
			continue
		}
		if fingerprint, err := ParseFingerprint(text); err == nil {
			return fingerprint
		}
	}
	return Fingerprint{}
}
