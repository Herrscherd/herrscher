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
	if elapsed := time.Since(started); elapsed < warningWriteBudget || elapsed > warningWriteBudget*3/2 {
		t.Fatalf("the write budget expired after %v, want about %v", elapsed.Round(time.Millisecond), warningWriteBudget)
	}
}
