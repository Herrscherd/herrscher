package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"syscall"
)

const (
	tailscaleSocketPath = "/var/run/tailscale/tailscaled.sock"
	tailscaleStatusURL  = "http://local-tailscaled.sock/localapi/v0/status"
	tailscaleMaxBody    = 4 << 20
)

// ErrTailscaleStatus reports a status request that tailscaled answered with a
// status other than 200 OK.
var ErrTailscaleStatus = errors.New("tailscale refused its status request")

// Tailscale lists the online peers of the tailnet through the local API of
// tailscaled. Without Tailscale on the machine, the socket is missing and the
// source is empty, not failed.
type Tailscale struct {
	Socket string
}

// NewTailscale returns the source that reads the default socket of tailscaled.
func NewTailscale() Tailscale {
	return Tailscale{Socket: tailscaleSocketPath}
}

// Name returns the label of the tailnet source.
func (Tailscale) Name() string {
	return "tailscale"
}

// Find returns one candidate per online peer of the tailnet. Peers are proposed
// as reachable machines, not as verified herrscher daemons.
func (source Tailscale) Find(ctx context.Context) ([]Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, SourceDeadline)
	defer cancel()

	body, err := source.status(ctx)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return nil, nil
	}
	return candidatesFromTailscaleStatus(body)
}

func (source Tailscale) status(ctx context.Context) ([]byte, error) {
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", source.Socket)
		},
	}}
	defer client.CloseIdleConnections()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tailscaleStatusURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		if isSocketAbsent(err) {
			return nil, nil
		}
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.Join(fmt.Errorf("%w: %s", ErrTailscaleStatus, response.Status), response.Body.Close())
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, tailscaleMaxBody))
	return body, errors.Join(readErr, response.Body.Close())
}

func isSocketAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
}

type tailscalePeer struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
}

type tailscaleStatus struct {
	Peer map[string]tailscalePeer `json:"Peer"`
}

func candidatesFromTailscaleStatus(body []byte) ([]Candidate, error) {
	var status tailscaleStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}
	var found []Candidate
	for _, peer := range status.Peer {
		if !peer.Online {
			continue
		}
		address := tailscaleAddress(peer)
		if address == "" {
			continue
		}
		found = append(found, Candidate{
			Name:    plainText(peer.HostName),
			Address: plainText(address),
			Route:   RouteTailnet,
		})
	}
	return found, nil
}

func tailscaleAddress(peer tailscalePeer) string {
	if name := strings.TrimSuffix(peer.DNSName, "."); name != "" {
		return name
	}
	if len(peer.TailscaleIPs) > 0 {
		return peer.TailscaleIPs[0]
	}
	return ""
}
