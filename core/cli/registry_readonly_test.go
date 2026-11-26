package cli

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestReadOnlyAnswersWhatTheCommandDeclared(t *testing.T) {
	reg := NewRegistry(clitext.NewMessages())
	run := func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil }
	for _, cmd := range []contracts.Cmd{contracts.New("acme", "peek").Reads().Do(run), contracts.New("acme", "poke").Do(run)} {
		if err := reg.Add(cmd); err != nil {
			t.Fatal(err)
		}
	}
	if !reg.ReadOnly([]string{"acme", "peek"}) || reg.ReadOnly([]string{"acme", "poke"}) || reg.ReadOnly([]string{"acme", "gone"}) {
		t.Fatal("ReadOnly does not follow the declarations")
	}
}
