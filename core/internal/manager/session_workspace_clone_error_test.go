package manager

import (
	"errors"
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/forge"
)

func TestCloneErrorMapsForgeFailuresToTheirCatalogueKey(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	cases := []struct {
		name     string
		cause    error
		sentinel error
		key      clitext.Key
	}{
		{name: "invalid repo spec", cause: forge.ErrRepoSpec, sentinel: forge.ErrRepoSpec, key: clitext.HostErrRepoSpec},
		{name: "config count", cause: forge.ErrConfigCount, sentinel: forge.ErrConfigCount, key: clitext.HostErrConfigCount},
		{name: "forge not connected", cause: contracts.ErrForgeNotConnected, sentinel: contracts.ErrForgeNotConnected, key: clitext.HostErrForgeNotConnected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.cloneError(fmt.Errorf("clone owner/repo: %w", tc.cause))
			want, catalogueErr := clitext.NewMessages().Text(i18n.English, tc.key, clitext.Params{})
			if catalogueErr != nil {
				t.Fatalf("catalogue misses %s: %v", tc.key, catalogueErr)
			}
			if err.Error() != want {
				t.Fatalf("cloneError = %q, want the %s text %q", err.Error(), tc.key, want)
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("cloneError lost the sentinel %v: %v", tc.sentinel, err)
			}
		})
	}
}
