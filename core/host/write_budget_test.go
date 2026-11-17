package host

import (
	"testing"
	"time"
)

func TestWriteBudgetExpiresAtTheWarningBudget(t *testing.T) {
	expired, stop := writeBudget()
	defer stop()
	started := time.Now()
	<-expired
	elapsed := time.Since(started)
	t.Logf("write budget expired after %d ms, want about %d ms", elapsed.Milliseconds(), warningWriteBudget.Milliseconds())
	if elapsed < warningWriteBudget || elapsed > warningWriteBudget*3/2 {
		t.Fatalf("the write budget expired after %d ms, want about %d ms", elapsed.Milliseconds(), warningWriteBudget.Milliseconds())
	}
}
