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

func newGatewayRenderer(gateway contracts.Gateway, channel string) *gatewayRenderer {
	return &gatewayRenderer{
		gw:   gateway,
		conv: contracts.Conversation{Gateway: gateway.Manifest().Kind, ID: channel},
	}
}

func (r *gatewayRenderer) handle(ctx context.Context, event contracts.Event) {
	if event.T != eventReply || !event.Done || event.Text == "" {
		return
	}
	for _, part := range chunk(event.Text, gatewayMaxLen) {
		_, postErr := r.gw.Post(ctx, r.conv, part)
		logCleanup(postErr)
	}
}

func chunk(text string, limit int) []string {
	var out []string
	runes := []rune(text)
	for len(runes) > limit {
		cut := limit
		for index := limit - 1; index > limit/2; index-- {
			if runes[index] == '\n' {
				cut = index
				break
			}
		}
		out = append(out, string(runes[:cut]))
		runes = runes[cut:]
		if len(runes) > 0 && runes[0] == '\n' {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}
