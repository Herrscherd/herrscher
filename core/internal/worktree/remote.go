package worktree

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Commander is the part of a runner this package needs: build the command that
// runs argv somewhere, and name that somewhere for an error message. Declared
// here, on the consumer side, so the worktree package depends on an idea rather
// than on the runner package.
type Commander interface {
	Command(ctx context.Context, dir string, argv ...string) *exec.Cmd
	Describe() string
}

// Remote is the worktree lifecycle on another machine. It implements the same
// port Worktreer does by calling `herrscher worktree` over there and reading its
// JSON: the filesystem work stays where the filesystem is, and one operation
// costs one round trip instead of ten.
type Remote struct {
	run        Commander
	bin        string // absolute path to herrscher on the far machine
	instanceID string
}

// NewRemote builds a Remote driving bin through run.
func NewRemote(run Commander, bin, instanceID string) *Remote {
	return &Remote{run: run, bin: bin, instanceID: instanceID}
}

// Branch is pure string work, so it is answered here. Paying a round trip to be
// told a name this process can compute would be a round trip for nothing.
func (r *Remote) Branch(name string) string { return branchFor(r.instanceID, name) }

func (r *Remote) Create(repo, name, base string) (string, error) {
	argv := []string{"worktree", "create", "--repo", repo, "--name", name, "--instance", r.instanceID}
	if base != "" {
		argv = append(argv, "--base", base)
	}
	var out struct {
		Path string `json:"path"`
	}
	if err := r.call(argv, &out); err != nil {
		return "", err
	}
	return out.Path, nil
}

// PreExisting reports whether Create would reuse a worktree. It has no error
// return, so a host that cannot answer is reported as "yes, one is there": that
// is the direction where a failed rollback leaves a directory behind, rather
// than the one where it deletes work it never created.
func (r *Remote) PreExisting(repo, name string) bool {
	var out struct {
		PreExisting bool `json:"preExisting"`
	}
	if err := r.call([]string{"worktree", "pre-existing", "--repo", repo, "--name", name, "--instance", r.instanceID}, &out); err != nil {
		return true
	}
	return out.PreExisting
}

func (r *Remote) Remove(repo, name string, force bool) error {
	argv := []string{"worktree", "remove", "--repo", repo, "--name", name, "--instance", r.instanceID}
	if force {
		argv = append(argv, "--force")
	}
	var out struct {
		Removed bool `json:"removed"`
	}
	return r.call(argv, &out)
}

// Materialize ships an agent's provisioning files into a remote worktree. The
// payload is a tar stream, which the verb on the far side extracts before
// adding the git excludes that materialization owns.
func (r *Remote) Materialize(ctx context.Context, worktreePath string, payload io.Reader) error {
	cmd := r.run.Command(ctx, "", r.bin, "worktree", "materialize", "--worktree", worktreePath)
	cmd.Stdin = payload
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("materialize on %s: %s", r.run.Describe(), reason(stderr.String(), err))
	}
	return nil
}

// call runs one worktree verb over there and decodes its single JSON line. The
// remote stderr is carried into the error verbatim: it is the git message, and
// nothing written here would be more precise.
func (r *Remote) call(argv []string, into any) error {
	cmd := r.run.Command(context.Background(), "", append([]string{r.bin}, argv...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	what := strings.Join(argv[:2], " ")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s on %s: %s", what, r.run.Describe(), reason(stderr.String(), err))
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), into); err != nil {
		return fmt.Errorf("%s on %s: unreadable answer %q", what, r.run.Describe(), stdout.String())
	}
	return nil
}

// reason prefers what the far side said over the local exit status, which only
// ever says that something failed.
func reason(stderr string, err error) string {
	if msg := strings.TrimSpace(stderr); msg != "" {
		return msg
	}
	return err.Error()
}
