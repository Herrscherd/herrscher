package ciloop

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBlockTagIsDrawnFromEntropy(t *testing.T) {
	block, err := NewBlock(bytes.NewReader(bytes.Repeat([]byte{0xab}, 8)), "unit", "https://example.test/run/1", "boom")
	if err != nil {
		t.Fatal(err)
	}
	if block.Tag != "abababababababab" {
		t.Fatalf("tag = %q, want the drawn bytes in hex", block.Tag)
	}
}

func TestBlockRedrawsATagTheLogAlreadyContains(t *testing.T) {
	entropy := io.MultiReader(bytes.NewReader(make([]byte, 8)), bytes.NewReader(bytes.Repeat([]byte{0x01}, 8)))
	block, err := NewBlock(entropy, "unit", "page", "log holds 0000000000000000 by chance")
	if err != nil {
		t.Fatal(err)
	}
	if block.Tag != "0101010101010101" {
		t.Fatalf("tag = %q, want the redrawn one", block.Tag)
	}
}

func TestBlockGivesUpWhenEntropyAlwaysCollides(t *testing.T) {
	entropy := bytes.NewReader(make([]byte, 8*fenceDrawLimit))
	if _, err := NewBlock(entropy, "unit", "page", "0000000000000000"); err == nil {
		t.Fatal("block built with a tag the body already contains")
	}
}

func TestBlockFailsWhenEntropyEnds(t *testing.T) {
	if _, err := NewBlock(bytes.NewReader(nil), "unit", "page", "log"); err == nil {
		t.Fatal("block built without entropy")
	}
}

func TestBlockKeepsTheLogUnescaped(t *testing.T) {
	block, err := NewBlock(bytes.NewReader(bytes.Repeat([]byte{0x02}, 8)), "unit", "page", "a -> b && c < d")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(block.Body, "a -> b && c < d") {
		t.Fatalf("log altered: %q", block.Body)
	}
}

func TestBlockReplacesInvalidUTF8(t *testing.T) {
	block, err := NewBlock(bytes.NewReader(bytes.Repeat([]byte{0x03}, 8)), "unit", "page", "ok\xff end")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(block.Body, "ok� end") {
		t.Fatalf("invalid byte kept: %q", block.Body)
	}
}
