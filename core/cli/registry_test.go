package cli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
)

func leaf(path ...string) contracts.Cmd {
	return contracts.New(path...).Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.DataResult("ran " + path[len(path)-1]), nil
	})
}

func TestAddRejectsDuplicatePath(t *testing.T) {
	var r cli.Registry
	if err := r.Add(leaf("set", "home")); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(leaf("set", "home")); err == nil {
		t.Fatal("duplicate path must be rejected")
	}
}

func TestAddRejectsEmptyPath(t *testing.T) {
	var r cli.Registry
	if err := r.Add(contracts.New().Do(nil)); err == nil {
		t.Fatal("empty path must be rejected")
	}
}

func dispatchText(r *cli.Registry, args []string) (string, error) {
	return cli.Plain(r.Dispatch(context.Background(), args))
}

func runText(r *cli.Registry, path []string, input contracts.Input) (string, error) {
	return cli.Plain(r.Run(context.Background(), path, input))
}

func build(t *testing.T, cmds ...contracts.Cmd) *cli.Registry {
	t.Helper()
	var r cli.Registry
	for _, c := range cmds {
		if err := r.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	return &r
}

func TestDispatchResolvesLongestPath(t *testing.T) {
	got := ""
	r := build(t,
		contracts.New("session", "list").Do(func(_ context.Context, _ contracts.Input) (contracts.Result, error) {
			got = "list"
			return contracts.Result{}, nil
		}),
		contracts.New("session", "create").Param("name", "", true).
			Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
				got = "create:" + in.Get("name")
				return contracts.Result{}, nil
			}),
	)
	if _, err := dispatchText(r, []string{"session", "create", "--name", "x"}); err != nil {
		t.Fatal(err)
	}
	if got != "create:x" {
		t.Fatalf("got %q", got)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	r := build(t, leaf("session", "list"))
	if _, err := dispatchText(r, []string{"nope"}); err == nil {
		t.Fatal("unknown command must error")
	}
}

func TestDispatchMissingRequiredParam(t *testing.T) {
	r := build(t, contracts.New("set", "home").Param("channel", "", true).
		Do(func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil }))
	if _, err := dispatchText(r, []string{"set", "home"}); err == nil {
		t.Fatal("missing required param must error")
	}
}

func TestDispatchBoolFlagAndRest(t *testing.T) {
	var in contracts.Input
	r := build(t, contracts.New("session", "create").
		Param("name", "", true).Param("shared", "", false).
		Do(func(_ context.Context, got contracts.Input) (contracts.Result, error) {
			in = got
			return contracts.Result{}, nil
		}))
	_, err := dispatchText(r, []string{"session", "create", "extra", "--name", "x", "--shared"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Get("name") != "x" || !in.Bool("shared") {
		t.Fatalf("args = %+v", in.Args)
	}
	if len(in.Rest) != 1 || in.Rest[0] != "extra" {
		t.Fatalf("rest = %v", in.Rest)
	}
}

func TestDispatchOptionalValueParamRequiresValueWhenPresent(t *testing.T) {
	r := build(t, contracts.New("session", "seed").
		ValueParam("turn_id", "", false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return contracts.DataResult(in.Get("turn_id")), nil
		}))

	if _, err := dispatchText(r, []string{"session", "seed", "--turn_id"}); err == nil ||
		!strings.Contains(err.Error(), "flag --turn_id needs a value") {
		t.Fatalf("valueless optional parameter error = %v", err)
	}
	got, err := dispatchText(r, []string{"session", "seed", "--turn_id", "true"})
	if err != nil || got != "true" {
		t.Fatalf("explicit value true = %q, %v; want preserved", got, err)
	}
}

func TestDispatchInlineValueCarriesAFlagLookingValue(t *testing.T) {
	r := build(t, contracts.New("session", "seed").
		Param("name", "", true).Param("task", "", true).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return contracts.DataResult(in.Get("name") + "|" + in.Get("task")), nil
		}))

	if _, err := dispatchText(r, []string{
		"session", "seed", "--name", "s", "--task", "--no-tests please",
	}); err == nil || !strings.Contains(err.Error(), "flag --task needs a value") {
		t.Fatalf("separate-token form error = %v; want the mistake still named", err)
	}

	got, err := dispatchText(r, []string{
		"session", "seed", "--name", "s", "--task=--no-tests please",
	})
	if err != nil || got != "s|--no-tests please" {
		t.Fatalf("inline form = %q, %v; want the value intact", got, err)
	}
}

func TestDispatchInlineValueEdgeCases(t *testing.T) {
	r := build(t, contracts.New("memory", "record").
		Param("key", "", true).Param("body", "", false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return contracts.DataResult(in.Get("key") + "|" + in.Get("body")), nil
		}))

	got, err := dispatchText(r, []string{"memory", "record", "--key=a/b", "--body=x=1"})
	if err != nil || got != "a/b|x=1" {
		t.Fatalf("got %q, %v; want the split to stop at the first =", got, err)
	}

	got, err = dispatchText(r, []string{"memory", "record", "--key=a/b", "--body="})
	if err != nil || got != "a/b|" {
		t.Fatalf("got %q, %v; want an empty body to count as given", got, err)
	}

	if _, err := dispatchText(r, []string{"memory", "record", "--nope=1"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag --nope") {
		t.Fatalf("unknown inline flag error = %v", err)
	}
}

func TestFlagArgUsesTheInlineFormOnlyWhenNeeded(t *testing.T) {
	if got := cli.FlagArg("text", "hello"); len(got) != 2 || got[0] != "--text" || got[1] != "hello" {
		t.Fatalf("ordinary value = %q, want the two-token form", got)
	}
	if got := cli.FlagArg("text", ""); len(got) != 2 || got[1] != "" {
		t.Fatalf("empty value = %q, want the two-token form", got)
	}
	if got := cli.FlagArg("text", "-p not a flag"); len(got) != 2 || got[1] != "-p not a flag" {
		t.Fatalf("single-dash value = %q; only -- confuses the parser", got)
	}
	if got := cli.FlagArg("task", "--no-tests"); len(got) != 1 || got[0] != "--task=--no-tests" {
		t.Fatalf("flag-looking value = %q, want the inline form", got)
	}
}

func TestFlagArgRoundTripsThroughDispatch(t *testing.T) {
	r := build(t, contracts.New("session", "send").
		Param("text", "", true).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return contracts.DataResult(in.Get("text")), nil
		}))

	for _, text := range []string{"hello", "--no-tests", "--", "x=1", "-p", "  spaced  ", "--a=--b"} {
		argv := append([]string{"session", "send"}, cli.FlagArg("text", text)...)
		got, err := dispatchText(r, argv)
		if err != nil || got != text {
			t.Fatalf("text %q came back %q, %v", text, got, err)
		}
	}
}

func TestDispatchReturnsHandlerOutput(t *testing.T) {
	r := build(t, leaf("session", "list"))
	out, err := dispatchText(r, []string{"session", "list"})
	if err != nil || out != "ran list" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestDispatchGlobalJSONFlag(t *testing.T) {
	r := &cli.Registry{}
	var gotJSON bool
	if err := r.Add(contracts.Cmd{
		Path:   []string{"session", "list"},
		Params: nil,
		Run: func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			gotJSON = in.JSON
			return contracts.DataResult("ok"), nil
		},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := dispatchText(r, []string{"session", "list", "--json"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !gotJSON {
		t.Fatal("expected in.JSON == true when --json passed")
	}
}

func TestHelpRendersSortedUsage(t *testing.T) {
	r := build(t,
		contracts.New("set", "home").Help("set home").Param("channel", "", true).
			Do(func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil }),
		contracts.New("session", "list").Help("list sessions").
			Do(func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil }),
	)
	h := r.Help()
	if want := "  session list"; !strings.Contains(h, want) {
		t.Fatalf("help missing %q:\n%s", want, h)
	}
	if !strings.Contains(h, "--channel <channel>") {
		t.Fatalf("help missing required-param rendering:\n%s", h)
	}
}

func TestRunInvokesCommandByExactPathWithTypedInput(t *testing.T) {
	var gotName string
	r := build(t,
		contracts.New("session", "create").Param("name", "", true).
			Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
				gotName = in.Get("name")
				return contracts.DataResult("created"), nil
			}),
	)
	out, err := runText(r, []string{"session", "create"},
		contracts.Input{Args: map[string]string{"name": "main"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "created" || gotName != "main" {
		t.Fatalf("Run did not pass typed input: out=%q name=%q", out, gotName)
	}
}

func TestRunRejectsUnknownPath(t *testing.T) {
	r := build(t, leaf("session", "list"))
	if _, err := runText(r, []string{"session", "nope"}, contracts.Input{}); err == nil {
		t.Fatal("Run must reject an unknown path")
	}
}

func TestRunChecksRequiredParams(t *testing.T) {
	r := build(t,
		contracts.New("session", "create").Param("name", "", true).
			Do(func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil }),
	)
	if _, err := runText(r, []string{"session", "create"}, contracts.Input{Args: make(map[string]string)}); err == nil {
		t.Fatal("Run must reject missing required param")
	}
}

func TestSpecsDescribeTheWholeRegistry(t *testing.T) {
	var r cli.Registry
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	okRun := func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil }
	must(r.Add(contracts.New("zulu").Help("last").Do(okRun)))
	must(r.Add(contracts.New("alpha", "one").
		Help("first").
		Param("name", "who", true).
		Do(okRun)))

	specs := r.Specs()
	if len(specs) != 2 {
		t.Fatalf("Specs = %+v, want both commands", specs)
	}
	if got := strings.Join(specs[0].Path, " "); got != "alpha one" {
		t.Fatalf("first spec = %q, want the alphabetical one", got)
	}
	if specs[0].Help != "first" || len(specs[0].Params) != 1 || specs[0].Params[0].Name != "name" {
		t.Fatalf("spec dropped declared shape: %+v", specs[0])
	}
}

func TestResolveReadsThePathAndTheFlagsWithoutRunning(t *testing.T) {
	ran := false
	var reg cli.Registry
	if err := reg.Add(contracts.New("session", "send").
		Param("name", "session name", true).
		Do(func(context.Context, contracts.Input) (contracts.Result, error) {
			ran = true
			return contracts.Result{}, nil
		})); err != nil {
		t.Fatal(err)
	}
	path, in, ok := reg.Resolve([]string{"session", "send", "--name", "revue", "hello"})
	if !ok {
		t.Fatal("Resolve must find a registered command")
	}
	if strings.Join(path, " ") != "session send" {
		t.Fatalf("path = %v, want [session send]", path)
	}
	if in.Get("name") != "revue" {
		t.Fatalf("name = %q, want revue", in.Get("name"))
	}
	if len(in.Rest) != 1 || in.Rest[0] != "hello" {
		t.Fatalf("Rest = %v, want [hello]", in.Rest)
	}
	if ran {
		t.Fatal("Resolve must not run the command")
	}
}

func TestResolveReportsAnUnknownCommand(t *testing.T) {
	var reg cli.Registry
	if _, _, ok := reg.Resolve([]string{"quantum", "entangle"}); ok {
		t.Fatal("Resolve must not claim to have found an unregistered command")
	}
}

func TestResolveNamesThePathEvenWhenTheFlagsDoNotParse(t *testing.T) {
	var reg cli.Registry
	if err := reg.Add(contracts.New("host", "add").
		Param("ssh", "ssh target", true).
		Do(func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil })); err != nil {
		t.Fatal(err)
	}
	path, _, ok := reg.Resolve([]string{"host", "add"})
	if !ok {
		t.Fatal("Resolve must find the command")
	}
	if strings.Join(path, " ") != "host add" {
		t.Fatalf("path = %v, want [host add]", path)
	}
}

func closedNotice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeSessionClosed, Args: []contracts.Arg{contracts.Text("Name", "demo")}}
}

type noticedError struct{ notice contracts.Notice }

func (e noticedError) Error() string            { return "internal english detail" }
func (e noticedError) Notice() contracts.Notice { return e.notice }

func TestTextRendersANoticeInTheRequestedLanguage(t *testing.T) {
	res := contracts.NoticeResult(closedNotice())
	en, err := cli.Text(res, i18n.English)
	if err != nil || !strings.Contains(en, "session demo closed") {
		t.Fatalf("English = %q (%v), want the English notice", en, err)
	}
	fr, err := cli.Text(res, i18n.French)
	if err != nil || !strings.Contains(fr, "fermée") || strings.Contains(fr, "closed") {
		t.Fatalf("French = %q (%v), want the French notice", fr, err)
	}
}

func TestLocalizeRendersANoticeErrorInTheRequestedLanguage(t *testing.T) {
	src := noticedError{notice: closedNotice()}
	out := cli.Localize(src, i18n.French)
	if !strings.Contains(out.Error(), "fermée") || strings.Contains(out.Error(), "internal english detail") {
		t.Fatalf("Localize = %q, want the French notice in place of the internal text", out.Error())
	}
	var kept noticedError
	if !errors.As(out, &kept) {
		t.Fatal("the rendered error must keep the original error in its chain")
	}
}

func TestZeroLanguageFallsBackToEnglish(t *testing.T) {
	res := contracts.NoticeResult(closedNotice())
	text, err := cli.Text(res, 0)
	if err != nil || !strings.Contains(text, "session demo closed") {
		t.Fatalf("Text with no language = %q (%v), want English", text, err)
	}
	out := cli.Localize(noticedError{notice: closedNotice()}, 0)
	if !strings.Contains(out.Error(), "session demo closed") {
		t.Fatalf("Localize with no language = %q, want English", out.Error())
	}
}

func TestRenderPicksTheErrorOrTheResult(t *testing.T) {
	text, err := cli.Render(contracts.DataResult("ok"), nil, i18n.French)
	if err != nil || text != "ok" {
		t.Fatalf("Render of a result = %q (%v), want ok", text, err)
	}
	_, err = cli.Render(contracts.Result{}, noticedError{notice: closedNotice()}, i18n.French)
	if err == nil || !strings.Contains(err.Error(), "fermée") {
		t.Fatalf("Render of a failure = %v, want the French error", err)
	}
}
