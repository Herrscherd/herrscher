package bridge

import (
	"context"
	"strconv"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

type noticeBackend struct{}

func (noticeBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: contracts.BackendKindNotice, Notice: &contracts.Notice{Code: contracts.CodeResumeLost}})
	return "done", nil
}
func (noticeBackend) Close() error { return nil }

func TestABackendNoticeReachesTheSinkAsItsCode(t *testing.T) {
	sink := &recordSink{}
	if err := runOneTurn(context.Background(), sink, noticeBackend{}, nil, contracts.Event{T: eventwire.EventInput, Text: "go"}, &turnController{}, nil, affordances{}, nil, 0); err != nil {
		t.Fatalf("runOneTurn: %v", err)
	}
	want := strconv.FormatUint(uint64(contracts.CodeResumeLost), 10)
	for _, event := range sink.events {
		if event.T == eventwire.EventBackendNotice && event.Value == want {
			return
		}
	}
	t.Fatalf("events = %+v, want a backend notice with code %s", sink.events, want)
}
