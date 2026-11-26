package host

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/selftools"
)

func TestEveryDaemonVerbIsATool(t *testing.T) {
	specs, err := operatorRegistryForTest(t).Specs(i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(commandSpecsJSON(specs))
	if err != nil {
		t.Fatal(err)
	}
	tools, _, err := selftools.Measure(raw, selftools.ExposureAll)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(specs) + 2; tools != want {
		t.Fatalf("all exposure lists %d tools for %d verbs, want one per verb and the two generic tools (%d)", tools, len(specs), want)
	}
}

func TestEveryCoreToolIsAVerbASessionRunsWithoutAsking(t *testing.T) {
	specs, err := operatorRegistryForTest(t).Specs(i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	registered := make([]string, 0, len(specs))
	for _, spec := range specs {
		registered = append(registered, strings.Join(spec.Path, " "))
	}
	for _, verb := range selftools.CoreVerbs() {
		path := strings.Fields(verb)
		if !slices.Contains(registered, verb) {
			t.Errorf("core tool %q names no verb of the daemon", verb)
		}
		if !authz.AgentRuns(path, false) {
			t.Errorf("core tool %q is a verb the agent role does not run: every call would become a question", verb)
		}
	}
}

func TestTheDaemonsOwnReadsAreNeverAnnounced(t *testing.T) {
	reg := operatorRegistryForTest(t)
	for _, verb := range []string{"session accounts", "session overview", "host check", "session list", "config get", "session get-mode"} {
		path := strings.Fields(verb)
		if authz.Writes(path, false, reg.ReadOnly(path)) {
			t.Errorf("%q would be announced as a write", verb)
		}
	}
}
