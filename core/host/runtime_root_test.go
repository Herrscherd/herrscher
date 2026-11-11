package host

import (
	"os"
	"testing"

	"github.com/Herrscherd/herrscher/core/runtimeguard"
)

func TestMain(m *testing.M) {
	guard := runtimeguard.Start(ProductionRuntime().Dir())
	os.Exit(guard.Finish(m.Run()))
}
