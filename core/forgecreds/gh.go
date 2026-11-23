package forgecreds

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	gitHubCLI            = "gh"
	gitHubCLITimeout     = 10 * time.Second
	gitHubCLIWaitDelay   = time.Second
	gitHubCLIOutputLimit = 4 << 10
)

// ErrCLIOutputTooLarge reports command line tool output beyond the bound, which
// no real token reaches.
var ErrCLIOutputTooLarge = errors.New("command line tool output is too large")

type boundedBuffer struct {
	buf bytes.Buffer
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.buf.Len()+len(data) > gitHubCLIOutputLimit {
		return 0, ErrCLIOutputTooLarge
	}
	return b.buf.Write(data)
}

func (b *boundedBuffer) String() string {
	return b.buf.String()
}

func gitHubCLIToken(ctx context.Context, host contracts.ForgeHost) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitHubCLITimeout)
	defer cancel()
	var output boundedBuffer
	cmd := exec.CommandContext(ctx, gitHubCLI, "auth", "token", "--hostname", host.String())
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = gitHubCLIWaitDelay
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(output.String()), nil
}
