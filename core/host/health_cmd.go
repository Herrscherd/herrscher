package host

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/health"
)

func healthVerb(source *healthSource) contracts.Cmd {
	return contracts.New("health").
		Help(clitext.CmdHelpHealth).
		Param(healthParamDeep, clitext.CmdHelpHealthDeep, false).
		Param(healthParamOrigin, clitext.CmdHelpHealthRepo, false).
		Param(healthParamWatch, clitext.CmdHelpHealthWatch, false).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			snapshot, err := source.run(ctx, in.Bool(healthParamDeep), in.Get(healthParamOrigin), in.Bool(healthParamWatch))
			if err != nil {
				return contracts.Result{}, err
			}
			raw, encodeErr := health.Encode(snapshot)
			return cli.Data(string(raw), encodeErr)
		})
}

const (
	healthParamDeep  = "deep"
	healthParamWatch = "watch"
)
