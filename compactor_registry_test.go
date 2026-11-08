package main

import (
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestTheJevCompactorIsCompiledIn(t *testing.T) {
	compactors := contracts.Default.Compactors()
	index := slices.IndexFunc(compactors, func(plugin contracts.Plugin) bool { return plugin.Manifest.Kind == "jev" })
	if index < 0 {
		t.Fatal("no compactor plugin of kind jev is registered in this build")
	}
	jev := compactors[index]
	if jev.Compactor == nil {
		t.Fatal("the jev plugin registers no Compactor factory")
	}
	required := slices.IndexFunc(jev.Manifest.Config, func(setting contracts.Setting) bool {
		return setting.Env == "TYPESAFE_API_KEY" && setting.Required
	})
	if required < 0 {
		t.Fatalf("jev settings = %+v, want TYPESAFE_API_KEY required", jev.Manifest.Config)
	}
}
