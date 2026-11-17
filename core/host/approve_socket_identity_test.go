//go:build !windows

package host

import (
	"context"
	"io"
	"os"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestSocketRefusesASessionAskingForAnotherSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	approvalTab := newApprovalTable(io.Discard)
	menus := &menuRecorder{menus: make(chan []contracts.Choice, 4)}
	sock := startApprovalSocket(ctx, t, newApprovalRegistry(t, approvalTab, menus))
	ownToken := os.Getenv(control.ApprovalKeyVar)
	argv := []string{"approve", "ask", "--session", "other", "--tool", "Bash", "--subject", "git push"}
	_, _, err := dispatchLiveCommand(withCallerToken(ctx, ownToken), newHostMessages(), isolatedRuntimeDir(t), sock, argv)
	if err == nil {
		t.Fatal("a session's token answered an approval for a different session over the socket")
	}
}
