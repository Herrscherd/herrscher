package host

import "testing"

func TestWaitLoggedNeverJoinsAfterTheBudgetExpired(t *testing.T) {
	logged := make(chan struct{})
	close(logged)
	budget := make(chan struct{})
	close(budget)
	for range 200 {
		if waitLogged(logged, budget) {
			t.Fatal("a write was joined after the budget had expired")
		}
	}
}
