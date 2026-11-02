package envx

import (
	"os"
	"testing"
)

func TestAHiddenKeyLeavesTheEnvironmentButStaysReadable(t *testing.T) {
	const key = "ENVX_TEST_TOKEN"
	t.Setenv(key, "s3cret")

	t.Cleanup(Hide([]string{key}))

	if _, set := os.LookupEnv(key); set {
		t.Fatal("the key is still in the environment every child process inherits")
	}
	if got := Getenv(key); got != "s3cret" {
		t.Fatalf("the value was lost: got %q", got)
	}
}

func TestARevealPutsBackWhatAnEarlierHideAlreadyHeld(t *testing.T) {
	const key = "ENVX_TEST_LAYERED"
	t.Setenv(key, "first")
	outer := Hide([]string{key})
	t.Cleanup(outer)

	t.Setenv(key, "second")
	inner := Hide([]string{key})
	inner()

	if got := Getenv(key); got != "first" {
		t.Fatalf("the reveal did not restore what the outer hide held: got %q", got)
	}
}

func TestAKeyNobodyHidStaysWhereItWas(t *testing.T) {
	const key = "ENVX_TEST_UNTOUCHED"
	t.Setenv(key, "operator-key")

	t.Cleanup(Hide([]string{"ENVX_TEST_OTHER"}))

	if got := os.Getenv(key); got != "operator-key" {
		t.Fatalf("a key outside the list was taken: got %q", got)
	}
}

func TestAnInvalidKeyDoesNotStopTheOthers(t *testing.T) {
	const key = "ENVX_TEST_MIXED"
	t.Setenv(key, "kept")

	reveal := Hide([]string{"", "ENVX_TEST=BAD", key})

	if _, set := os.LookupEnv(key); set {
		t.Fatal("a valid key next to an invalid one stayed in the environment")
	}
	reveal()
	if got := os.Getenv(key); got != "kept" {
		t.Fatalf("the reveal did not restore the valid key: got %q", got)
	}
}
