package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/service"
)

func addHostCommands(reg *cli.Registry, st *state.State) error {
	if err := reg.Add(contracts.New("host", "add").
		Help("register a machine sessions can run on, and provision herrscher there").
		ValueParam("name", "host name (unique; `local` is reserved); also takes a bare argument", false).
		Param("ssh", "ssh target, e.g. me@build1", true).
		Param("workspace", "absolute path to the workspace root over there", true).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			h := state.Host{
				Name:      hostNameOf(in),
				SSH:       strings.TrimSpace(in.Get("ssh")),
				Workspace: strings.TrimSpace(in.Get("workspace")),
			}
			if h.Name == "" {
				return "", errHostNameMissing
			}
			if h.Name == state.LocalHost {
				return "", errLocalHostReserved
			}
			if h.SSH == "" {
				return "", &hostNeedsSSHError{Name: h.Name}
			}
			if !strings.HasPrefix(h.Workspace, "/") {
				return "", &hostWorkspaceError{Name: h.Name, Workspace: h.Workspace}
			}
			if _, exists := st.FindHost(h.Name); exists {
				return "", &hostExistsError{Name: h.Name}
			}
			provisioned, err := provisionHost(ctx, runnerFor(h), h, st.SourceDir())
			if err != nil {
				return "", err
			}
			if err := st.PutHost(provisioned); err != nil {
				return "", err
			}
			return fmt.Sprintf("registered %s (%s, %s/%s, herrscher %s)", provisioned.Name, provisioned.SSH, provisioned.GOOS, provisioned.GOARCH, provisioned.Version), nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("host", "list").
		Help("list the places a session can run").
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			hosts := st.SnapshotHosts()
			sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })
			if in.JSON {
				b, err := json.Marshal(hosts)
				return string(b), err
			}
			var b strings.Builder
			b.WriteString("- local (this machine)\n")
			for _, h := range hosts {
				fmt.Fprintf(&b, "- %s: %s, workspace %s, herrscher %s (%s/%s)\n",
					h.Name, h.SSH, h.Workspace, orAbsent(h.Version), h.GOOS, h.GOARCH)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("host", "check").
		Help("say whether a host can carry a session: ssh, herrscher, workspace, git").
		ValueParam("name", "host name; also takes a bare argument", false).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			name := hostNameOf(in)
			h, ok := st.FindHost(name)
			if !ok {
				return "", unknownHost(st, name)
			}
			rep := checkHost(ctx, runnerFor(h), h, sourceVersionOf(ctx, st))
			if in.JSON {
				b, err := json.Marshal(rep)
				return string(b), err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s (%s)\n", rep.Host, rep.SSH)
			fmt.Fprintf(&b, "- ssh: %s\n", yesNo(rep.Reachable))
			fmt.Fprintf(&b, "- herrscher: %s\n", orAbsent(rep.Herrscher))
			fmt.Fprintf(&b, "- workspace: %s\n", yesNo(rep.Workspace))
			fmt.Fprintf(&b, "- git: %s", yesNo(rep.Git))
			for _, n := range rep.Notes {
				fmt.Fprintf(&b, "\n  %s", n)
			}
			return b.String(), nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("host", "provision").
		Help("rebuild and reinstall herrscher on a host from the configured source").
		ValueParam("name", "host name; also takes a bare argument", false).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			name := hostNameOf(in)
			h, ok := st.FindHost(name)
			if !ok {
				return "", unknownHost(st, name)
			}
			provisioned, err := provisionHost(ctx, runnerFor(h), h, st.SourceDir())
			if err != nil {
				return "", err
			}
			if err := st.PutHost(provisioned); err != nil {
				return "", err
			}
			return fmt.Sprintf("provisioned %s with herrscher %s (%s/%s)", provisioned.Name, provisioned.Version, provisioned.GOOS, provisioned.GOARCH), nil
		})); err != nil {
		return err
	}

	return reg.Add(contracts.New("host", "rm").
		Help("forget a host (refused while sessions still run on it)").
		ValueParam("name", "host name; also takes a bare argument", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			name := hostNameOf(in)
			if _, ok := st.FindHost(name); !ok {
				return "", unknownHost(st, name)
			}
			var carried []string
			for _, s := range st.SnapshotSessions() {
				if s.Host == name {
					carried = append(carried, s.Name)
				}
			}
			if len(carried) > 0 {
				return "", &hostCarriesError{Name: name, Sessions: strings.Join(carried, ", ")}
			}
			if _, err := st.RemoveHost(name); err != nil {
				return "", err
			}
			return "forgot " + name, nil
		}))
}

func hostNameOf(in contracts.Input) string { return firstOf(in.Get("name"), in.Rest) }

func unknownHost(st *state.State, name string) error {
	var known []string
	for _, h := range st.SnapshotHosts() {
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

func orAbsent(s string) string {
	if s == "" {
		return "absent"
	}
	return s
}
