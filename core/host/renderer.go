package host

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const gatewayMaxLen = 2000

type gatewayRenderer struct {
	gw   contracts.Gateway
	conv contracts.Conversation
}

func newGatewayRenderer(gw contracts.Gateway, ch string) *gatewayRenderer {
	return &gatewayRenderer{
		gw:   gw,
		conv: contracts.Conversation{Gateway: gw.Manifest().Kind, ID: ch},
	}
}

func (r *gatewayRenderer) handle(ctx context.Context, e contracts.Event) {
	if e.T != eventReply || !e.Done || e.Text == "" {
		return
	}
	for _, part := range chunk(e.Text, gatewayMaxLen) {
		_, postErr := r.gw.Post(ctx, r.conv, part)
		logCleanup(postErr)
	}
}

func chunk(s string, limit int) []string {
	var out []string
	r := []rune(s)
	for len(r) > limit {
		cut := limit
		for i := limit - 1; i > limit/2; i-- {
			if r[i] == '\n' {
				cut = i
				break
			}
		}
		out = append(out, string(r[:cut]))
		r = r[cut:]
		if len(r) > 0 && r[0] == '\n' {
			r = r[1:]
		}
	}
	if len(r) > 0 {
		out = append(out, string(r))
	}
	return out
}
