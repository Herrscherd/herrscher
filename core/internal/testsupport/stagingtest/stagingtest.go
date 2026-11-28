// Package stagingtest plants the staging files a crashed write leaves behind, and checks whether a sweep removed them, for tests of the sweep sites.
package stagingtest

import (
	"os"
	"testing"
	"time"
)

const stagingMode = 0o600

// Plant writes a staging file at path and sets its modification time to age before now, so a sweep sees it as stale when age is past the sweep threshold.
func Plant(tb testing.TB, path string, age time.Duration) {
	tb.Helper()
	if err := os.WriteFile(path, []byte("x"), stagingMode); err != nil {
		tb.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		tb.Fatal(err)
	}
}

// Present fails the test when path is missing.
func Present(tb testing.TB, path string) {
	tb.Helper()
	if _, err := os.Stat(path); err != nil {
		tb.Fatalf("%s was removed, want it kept: %v", path, err)
	}
}

// Absent fails the test when path is still there.
func Absent(tb testing.TB, path string) {
	tb.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		tb.Fatalf("%s survived, want it swept: %v", path, err)
	}
}
