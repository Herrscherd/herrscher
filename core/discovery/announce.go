package discovery

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/hashicorp/mdns"
)

// MaxAnnounceWindow bounds how long an announcement may stay on the network.
const MaxAnnounceWindow = 10 * time.Minute

const (
	txtProtocol     = "proto=1"
	instancePrefix  = "herrscher-"
	localHostSuffix = ".local."
)

// ErrWindowOutOfRange reports an announcement window that is not positive or that
// exceeds MaxAnnounceWindow.
var ErrWindowOutOfRange = errors.New("announcement window must be positive and at most ten minutes")

// Announcement is what a daemon publishes on the local network while a pairing
// window is open: its presence, its fingerprint and the port it listens on. It
// carries no user name, no session name and no build version.
type Announcement struct {
	Fingerprint Fingerprint
	Port        int
	Addresses   []net.IP
}

// TXT returns the only text fields an announcement publishes.
func (a Announcement) TXT() []string {
	return []string{txtProtocol, fingerprintField + a.Fingerprint.String()}
}

func (a Announcement) zone() (*mdns.MDNSService, error) {
	instance := instancePrefix + a.Fingerprint.String()
	return mdns.NewMDNSService(instance, ServiceType, serviceDomain+".", instance+localHostSuffix, a.Port, a.Addresses, a.TXT())
}

// Serve publishes an announcement until ctx ends, then withdraws it.
type Serve func(ctx context.Context, a Announcement) error

// Advertise publishes the announcement for at most window and returns once the
// window closes or serve returns. The window is the only way an announcement ends
// early or late: nothing stays on the network after it.
func Advertise(ctx context.Context, a Announcement, window time.Duration, serve Serve) error {
	if a.Fingerprint.IsZero() {
		return ErrFingerprintInvalid
	}
	if window <= 0 || window > MaxAnnounceWindow {
		return ErrWindowOutOfRange
	}
	ctx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	return serve(ctx, a)
}

// ServeMDNS answers the DNS-SD questions for the announcement until ctx ends. The
// host name it publishes is derived from the fingerprint, never from the machine.
func ServeMDNS(ctx context.Context, a Announcement) error {
	zone, err := a.zone()
	if err != nil {
		return err
	}
	server, err := mdns.NewServer(&mdns.Config{Zone: zone})
	if err != nil {
		return err
	}
	<-ctx.Done()
	return server.Shutdown()
}
