package host

import (
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/service"
)

const stopBridgeDrainMargin = 10 * time.Second

func TestServiceStopTimeoutCoversTheStopPath(t *testing.T) {
	worstStop := 6*defaultTeardownGrace + persistWriteGrace
	if service.StopTimeout-worstStop < stopBridgeDrainMargin {
		t.Fatalf("the service kills the daemon after %v, which leaves less than %v beyond the worst stop path of %v", service.StopTimeout, stopBridgeDrainMargin, worstStop)
	}
}
