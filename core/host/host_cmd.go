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

// addHostCommands registers the five `host` verbs. Like the schedule family,
// they are neutral argv: a chat gateway binds them as they are, and core never
// learns which gateway that is.
func addHostCommands(reg *cli.Registry, st *state.State) error {
	if err := reg.Add(contracts.New("host", "add").
		Help("register a machine sessions can run on, and provision herrscher there").
		Param("name", "host name (unique; `local` is reserved)", true).
		Param("ssh", "ssh target, e.g. me@build1", true).
		Param("workspace", "absolute path to the workspace root over there", true).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			h := state.Host{
				Name:      strings.TrimSpace(in.Get("name")),
				SSH:       strings.TrimSpace(in.Get("ssh")),
				Workspace: strings.TrimSpace(in.Get("workspace")),
			}
			if h.Name == "" {
				return "", fmt.Errorf("a host needs a name")
			}
			if h.Name == state.LocalHost {
				return "", fmt.Errorf("`local` is this machine and always exists: it cannot be registered")
			}
			if h.SSH == "" {
				return "", fmt.Errorf("host %q needs an ssh target, e.g. --ssh me@%s", h.Name, h.Name)
			}
			if !strings.HasPrefix(h.Workspace, "/") {
				return "", fmt.Errorf("host %q needs an absolute workspace path, got %q", h.Name, h.Workspace)
			}
			if _, exists := st.FindHost(h.Name); exists {
				return "", fmt.Errorf("a host named %q already exists (remove it first, or run `host provision %s`)", h.Name, h.Name)
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
			// local is listed but not stored: it is where a session runs when it
			// names no host, so a list that omitted it would not answer the
			// question it was asked.
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
		Param("name", "host name", true).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			h, ok := st.FindHost(in.Get("name"))
			if !ok {
				return "", unknownHost(st, in.Get("name"))
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
		Param("name", "host name", true).
		Do(func(ctx context.Context, in contracts.Input) (string, error) {
			h, ok := st.FindHost(in.Get("name"))
			if !ok {
				return "", unknownHost(st, in.Get("name"))
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
		Param("name", "host name", true).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			name := in.Get("name")
			// It closes nothing on its own, the way `schedule rm` does not close
			// the session it opened. Naming them is the operator's next move.
			var carried []string
			for _, s := range st.SnapshotSessions() {
				if s.Host == name {
					carried = append(carried, s.Name)
				}
			}
			if len(carried) > 0 {
				return "", fmt.Errorf("host %q still carries %s: close them first", name, strings.Join(carried, ", "))
			}
			found, err := st.RemoveHost(name)
			if err != nil {
				return "", err
			}
			if !found {
				return "", unknownHost(st, name)
			}
			return "forgot " + name, nil
		}))
}

// unknownHost names what does exist, because the usual cause is a typo.
func unknownHost(st *state.State, name string) error {
	var known []string
	for _, h := range st.SnapshotHosts() {
		known = append(known, h.Name)
	}
	if len(known) == 0 {
		return fmt.Errorf("no host named %q, and none is registered: add one with `host add <name> --ssh <target> --workspace <path>`", name)
	}
	return fmt.Errorf("no host named %q (known: %s)", name, strings.Join(known, ", "))
}

// sourceVersionOf is what this daemon would provision today, or "" when it has
// no source checkout to compare against.
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
