package prwatch

import (
	"context"
	"errors"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// ErrFeedRefused reports that the forge refused the event feed of a pull request. A watch
// stops asking for the feed for a pause and keeps reading the request on its cadence.
var ErrFeedRefused = errors.New("pull request event feed refused by the forge")

// Feed is the event stream of a forge, when the forge grants one. Subscribe returns a
// channel that receives a value for each change of the pull request, and is closed when
// the stream ends. The stream stops when ctx is done. A refusal wraps [ErrFeedRefused].
type Feed interface {
	Subscribe(ctx context.Context, ref contracts.PullRequestRef) (<-chan struct{}, error)
}
