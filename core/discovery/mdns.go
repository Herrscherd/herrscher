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
	entryBufferSize  = 64
	fingerprintField = "fp="
)

// MDNSSource browses the local network for announced daemons during Window. The
// browse only sends multicast questions: it changes nothing on the network.
type MDNSSource struct {
	Window time.Duration
}

// Name returns the label of the DNS-SD source.
func (MDNSSource) Name() string {
	return "dns-sd"
}

// Find browses the service for the window and returns one candidate per announced
// daemon that answered. The browse ends on its own: a silent network is an empty list.
func (source MDNSSource) Find(ctx context.Context) ([]Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, source.Window)
	defer cancel()

	entries := make(chan *mdns.ServiceEntry, entryBufferSize)
	err := mdns.QueryContext(ctx, &mdns.QueryParam{
		Service: ServiceType,
		Domain:  serviceDomain,
		Timeout: source.Window,
		Entries: entries,
	})
	close(entries)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}

	var found []Candidate
	for entry := range entries {
		if candidate, ok := candidateFromEntry(entry); ok {
			found = append(found, candidate)
		}
	}
	return found, nil
}

func candidateFromEntry(entry *mdns.ServiceEntry) (Candidate, bool) {
	address := addressOf(entry)
	if address == "" {
		return Candidate{}, false
	}
	return Candidate{
		Name:        strings.TrimSuffix(entry.Name, serviceSuffix),
		Address:     address,
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
