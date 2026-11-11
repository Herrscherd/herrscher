package host

import (
	"errors"
	"testing"
)

var errLocalProductionRuntime = errors.New("remote runtime dir is the local production runtime, refusing to remove it")

func refuseLocalProductionRuntime(dir string) error {
	if dir == ProductionRuntime().Dir() {
		return errLocalProductionRuntime
	}
	return nil
}

func TestRemoteRuntimeDirNeverIsTheLocalProductionRuntime(t *testing.T) {
	if err := refuseLocalProductionRuntime(ProductionRuntime().Dir()); err == nil {
		t.Fatal("the local production runtime dir was accepted as a remote cleanup target")
	}
	if err := refuseLocalProductionRuntime("/tmp/herrscher-4242"); err != nil {
		t.Fatalf("a distinct remote dir was refused: %v", err)
	}
}
