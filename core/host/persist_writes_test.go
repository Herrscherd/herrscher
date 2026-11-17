package host

import "testing"

func TestNilPersistWritesIsSafe(t *testing.T) {
	var persists *persistWrites
	end := persists.begin()
	end()
	persists.stop()
}
