package forge

import (
	"context"
	"strconv"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type sizedListing struct {
	sizes []int
}

func (l *sizedListing) fetch(_ context.Context, page contracts.PageRequest) (contracts.Page[int], error) {
	l.sizes = append(l.sizes, page.Limit)
	start := 0
	if page.Cursor != "" {
		parsed, err := strconv.Atoi(string(page.Cursor))
		if err != nil {
			return contracts.Page[int]{}, err
		}
		start = parsed
	}
	items := make([]int, 0, page.Limit)
	for index := start; index < start+page.Limit-10; index++ {
		items = append(items, index)
	}
	return contracts.Page[int]{Items: items, Next: contracts.PageCursor(strconv.Itoa(start + page.Limit))}, nil
}

func TestCollectUpToAsksEveryPageAtTheSameSizeAndCutsAtTheLimit(t *testing.T) {
	listing := &sizedListing{}
	items, truncated, err := CollectUpTo(t.Context(), 150, listing.fetch)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 150 || !truncated {
		t.Fatalf("got %d items, truncated %v, want 150 and truncated", len(items), truncated)
	}
	seen := make(map[int]bool, len(items))
	for _, item := range items {
		if seen[item] {
			t.Fatalf("item %d listed twice", item)
		}
		seen[item] = true
	}
	for _, size := range listing.sizes {
		if size != contracts.MaxPageLimit {
			t.Fatalf("page sizes = %v, want every page at %d", listing.sizes, contracts.MaxPageLimit)
		}
	}
}
