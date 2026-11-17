package host

import "testing"

func TestNilPersistWritesIsSafe(*testing.T) {
	var persists *persistWrites
	end := persists.begin()
	end()
	persists.stop()
}
