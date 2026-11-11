package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/service"
)

func addHostCommands(reg *cli.Registry, hostState *state.State, layout RuntimeLayout) error {
	return addVerbs(reg,
		hostAddVerb(hostState, layout),
		hostListVerb(hostState),
		hostCheckVerb(hostState, layout),
		hostProvisionVerb(hostState, layout),
		hostRmVerb(hostState),
	)
}

func hostAddVerb(store *state.State, layout RuntimeLayout) contracts.Cmd {
	return contracts.New("host", "add").
		Help(clitext.CmdHelpHostAdd).
		ValueParam(paramName, clitext.CmdParamHostAddName, false).
		Param("ssh", clitext.CmdParamHostAddSSH, true).
		Param("workspace", clitext.CmdParamHostAddWorkspace, true).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(addHost(ctx, layout, store, in))
		})
}

func addHost(ctx context.Context, layout RuntimeLayout, hostState *state.State, input contracts.Input) (string, error) {
	host := state.Host{
		Name:      hostNameOf(input),
		SSH:       strings.TrimSpace(input.Get("ssh")),
		Workspace: strings.TrimSpace(input.Get("workspace")),
	}
	if err := checkNewHost(hostState, host); err != nil {
		return "", err
	}
	run, err := runnerFor(layout, host)
	if err != nil {
		return "", err
	}
	provisioned, err := provisionHost(ctx, run, host, hostState.SourceDir())
	if err != nil {
		return "", err
	}
	if err := hostState.PutHost(provisioned); err != nil {
		return "", err
	}
	return fmt.Sprintf("registered %s (%s, %s/%s, herrscher %s)", provisioned.Name, provisioned.SSH, provisioned.GOOS, provisioned.GOARCH, provisioned.Version), nil
}

func checkNewHost(hostState *state.State, host state.Host) error {
	if host.Name == "" {
		return errHostNameMissing
	}
	if host.Name == state.LocalHost {
		return errLocalHostReserved
	}
	if host.SSH == "" {
		return &hostNeedsSSHError{Name: host.Name}
	}
	if !strings.HasPrefix(host.Workspace, "/") {
		return &hostWorkspaceError{Name: host.Name, Workspace: host.Workspace}
	}
	if _, exists := hostState.FindHost(host.Name); exists {
		return &hostExistsError{Name: host.Name}
	}
	return nil
}

func hostListVerb(store *state.State) contracts.Cmd {
	return contracts.New("host", "list").
		Help(clitext.CmdHelpHostList).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(listHosts(store, in.JSON))
		})
}

func listHosts(st *state.State, asJSON bool) (string, error) {
	hosts := st.SnapshotHosts()
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })
	if asJSON {
		body, err := json.Marshal(hosts)
		return string(body), err
	}
	var body strings.Builder
	_, _ = body.WriteString("- local (this machine)\n")
	for _, host := range hosts {
		fmt.Fprintf(&body, "- %s: %s, workspace %s, herrscher %s (%s/%s)\n",
			host.Name, host.SSH, host.Workspace, orAbsent(host.Version), host.GOOS, host.GOARCH)
	}
	return strings.TrimRight(body.String(), "\n"), nil
}

func hostCheckVerb(store *state.State, layout RuntimeLayout) contracts.Cmd {
	return contracts.New("host", "check").
		Help(clitext.CmdHelpHostCheck).
		ValueParam(paramName, clitext.CmdParamHostCheckName, false).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(checkRegisteredHost(ctx, layout, store, in))
		})
}

func checkRegisteredHost(ctx context.Context, layout RuntimeLayout, store *state.State, input contracts.Input) (string, error) {
	name := hostNameOf(input)
	host, ok := store.FindHost(name)
	if !ok {
		return "", unknownHost(store, name)
	}
	run, err := runnerFor(layout, host)
	if err != nil {
		return "", err
	}
	rep := checkHost(ctx, run, host, sourceVersionOf(ctx, store))
	if input.JSON {
		b, err := json.Marshal(rep)
		return string(b), err
	}
	return formatHostReport(rep), nil
}

func formatHostReport(rep hostReport) string {
	var body strings.Builder
	fmt.Fprintf(&body, "%s (%s)\n", rep.Host, rep.SSH)
	fmt.Fprintf(&body, "- ssh: %s\n", yesNo(rep.Reachable))
	fmt.Fprintf(&body, "- herrscher: %s\n", orAbsent(rep.Herrscher))
	fmt.Fprintf(&body, "- workspace: %s\n", yesNo(rep.Workspace))
	fmt.Fprintf(&body, "- git: %s", yesNo(rep.Git))
	for _, n := range rep.Notes {
		fmt.Fprintf(&body, "\n  %s", n)
	}
	return body.String()
}

func hostProvisionVerb(store *state.State, layout RuntimeLayout) contracts.Cmd {
	return contracts.New("host", "provision").
		Help(clitext.CmdHelpHostProvision).
		ValueParam(paramName, clitext.CmdParamHostProvisionName, false).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(provisionRegisteredHost(ctx, layout, store, in))
		})
}

func provisionRegisteredHost(ctx context.Context, layout RuntimeLayout, store *state.State, in contracts.Input) (string, error) {
	name := hostNameOf(in)
	host, ok := store.FindHost(name)
	if !ok {
		return "", unknownHost(store, name)
	}
	run, err := runnerFor(layout, host)
	if err != nil {
		return "", err
	}
	provisioned, err := provisionHost(ctx, run, host, store.SourceDir())
	if err != nil {
		return "", err
	}
	if err := store.PutHost(provisioned); err != nil {
		return "", err
	}
	return fmt.Sprintf("provisioned %s with herrscher %s (%s/%s)", provisioned.Name, provisioned.Version, provisioned.GOOS, provisioned.GOARCH), nil
}

func hostRmVerb(store *state.State) contracts.Cmd {
	return contracts.New("host", "rm").
		Help(clitext.CmdHelpHostRm).
		ValueParam(paramName, clitext.CmdParamHostRmName, false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(forgetHost(store, in))
		})
}

func forgetHost(store *state.State, in contracts.Input) (string, error) {
	name := hostNameOf(in)
	if _, ok := store.FindHost(name); !ok {
		return "", unknownHost(store, name)
	}
	if carried := sessionsOnHost(store, name); len(carried) > 0 {
		return "", &hostCarriesError{Name: name, Sessions: strings.Join(carried, ", ")}
	}
	if _, err := store.RemoveHost(name); err != nil {
		return "", err
	}
	return "forgot " + name, nil
}

func sessionsOnHost(store *state.State, name string) []string {
	var carried []string
	sessions := store.SnapshotSessions()
	for index := range sessions {
		if sessions[index].Host == name {
			carried = append(carried, sessions[index].Name)
		}
	}
	return carried
}

func hostNameOf(in contracts.Input) string { return firstOf(in.Get(paramName), in.Rest) }

func unknownHost(hostState *state.State, name string) error {
	var known []string
	for _, h := range hostState.SnapshotHosts() {
		known = append(known, h.Name)
	}
	if name == "" {
		if len(known) == 0 {
			return errHostNoneRegistered
		}
		return &hostNameRequiredError{Known: strings.Join(known, ", ")}
	}
	if len(known) == 0 {
		return &hostNotFoundError{Name: name}
	}
	return &hostNotFoundError{Name: name, Known: strings.Join(known, ", ")}
}

func sourceVersionOf(ctx context.Context, st *state.State) string {
	src := st.SourceDir()
	if src == "" {
		return ""
	}
	return service.SourceVersion(ctx, src)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orAbsent(text string) string {
	if text == "" {
		return "absent"
	}
	return text
}
