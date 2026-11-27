package drives

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRclone      = "rclone"
	rcloneEnvPrefix    = "RCLONE_"
	listOutputBytes    = 8 << 20
	stderrBytes        = 4 << 10
	detailRunes        = 300
	exitDirNotFound    = 3
	exitFileNotFound   = 4
	rcloneWaitDelay    = 5 * time.Second
	versionOutputBytes = 4 << 10
)

// ErrNoSuchFile reports a path the remote does not hold.
var ErrNoSuchFile = errors.New("no such file or folder on the drive")

// RcloneError reports an rclone run that failed, with the last line it wrote on stderr.
type RcloneError struct {
	Op     string
	Code   int
	Detail string
	Cause  error
}

func (e *RcloneError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("rclone %s failed: %v", e.Op, e.Cause)
	}
	return fmt.Sprintf("rclone %s failed: %s", e.Op, e.Detail)
}

func (e *RcloneError) Unwrap() error { return e.Cause }

// TooLargeError reports output or input larger than herrscher accepts.
type TooLargeError struct {
	Limit int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("larger than the %d bytes herrscher accepts", e.Limit)
}

// Rclone runs rclone against the configuration file herrscher owns, with every
// RCLONE_ variable of the environment left out, so the operator's own rclone
// setup never leaks in.
type Rclone struct {
	argv   []string
	config string
}

// NewRclone builds the runner. command is split on spaces, and an empty one runs rclone from PATH.
func NewRclone(command, config string) Rclone {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		fields = []string{defaultRclone}
	}
	return Rclone{argv: fields, config: config}
}

// Command is the rclone command the runner starts.
func (r Rclone) Command() string { return strings.Join(r.argv, " ") }

// Item is one item of a folder listing.
type Item struct {
	Path    string    `json:"Path"`
	Name    string    `json:"Name"`
	Size    int64     `json:"Size"`
	ModTime time.Time `json:"ModTime"`
	IsDir   bool      `json:"IsDir"`
}

// List lists one level of target.
func (r Rclone) List(ctx context.Context, target string) ([]Item, error) {
	out, err := r.run(ctx, "lsjson", nil, listOutputBytes, "lsjson", "--max-depth", "1", "--no-mimetype", "--", target)
	if err != nil {
		return nil, err
	}
	var entries []Item
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, &RcloneError{Op: "lsjson", Cause: err}
	}
	return entries, nil
}

// Read returns the content of target, refused past limit bytes.
func (r Rclone) Read(ctx context.Context, target string, limit int64) ([]byte, error) {
	return r.run(ctx, "cat", nil, limit, "cat", "--count", strconv.FormatInt(limit+1, 10), "--", target)
}

// Write stores size bytes read from body at target.
func (r Rclone) Write(ctx context.Context, target string, body io.Reader, size int64) error {
	_, err := r.run(ctx, "rcat", body, 0, "rcat", "--size", strconv.FormatInt(size, 10), "--", target)
	return err
}

// Version is the first line of rclone version.
func (r Rclone) Version(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "version", nil, versionOutputBytes, "version")
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(first), nil
}

// DeleteRemote removes the remote name from the configuration.
func (r Rclone) DeleteRemote(ctx context.Context, name string) error {
	_, err := r.run(ctx, "config delete", nil, 0, "config", "delete", name)
	return err
}

// Terminal is the standard streams an interactive rclone run is attached to.
type Terminal struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// CreateRemote runs rclone config create in the terminal, which signs in through the browser for an OAuth kind.
func (r Rclone) CreateRemote(ctx context.Context, name, rcloneType string, params []string, term Terminal) error {
	args := append([]string{"config", "create", name, rcloneType}, params...)
	cmd := r.prepare(ctx, args)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = term.In, term.Out, term.Err
	if err := cmd.Run(); err != nil {
		return &RcloneError{Op: "config create", Code: exitCode(err), Cause: err}
	}
	return nil
}

func (r Rclone) prepare(ctx context.Context, args []string) *exec.Cmd {
	argv := append(append(append([]string{}, r.argv[1:]...), "--config", r.config), args...)
	cmd := exec.CommandContext(ctx, r.argv[0], argv...)
	cmd.Env = withoutRcloneVariables(os.Environ())
	cmd.WaitDelay = rcloneWaitDelay
	return cmd
}

func (r Rclone) run(ctx context.Context, op string, stdin io.Reader, limit int64, args ...string) ([]byte, error) {
	cmd := r.prepare(ctx, append([]string{"--ask-password=false"}, args...))
	if stdin == nil {
		stdin = bytes.NewReader(nil)
	}
	cmd.Stdin = stdin
	out := &cappedBuffer{limit: limit}
	errOut := &cappedBuffer{limit: stderrBytes, keepTail: true}
	cmd.Stdout, cmd.Stderr = out, errOut
	runErr := cmd.Run()
	if out.over {
		return nil, &TooLargeError{Limit: limit}
	}
	if runErr != nil {
		return nil, rcloneFailure(op, runErr, errOut.String())
	}
	return out.Bytes(), nil
}

func rcloneFailure(op string, runErr error, stderr string) error {
	code := exitCode(runErr)
	failure := &RcloneError{Op: op, Code: code, Detail: lastLine(stderr), Cause: runErr}
	if code == exitDirNotFound || code == exitFileNotFound {
		failure.Cause = errors.Join(ErrNoSuchFile, runErr)
	}
	return failure
}

func exitCode(err error) int {
	if exited, ok := errors.AsType[*exec.ExitError](err); ok {
		return exited.ExitCode()
	}
	return 0
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if _, after, found := strings.Cut(line, " : "); found {
		line = after
	} else if fields := strings.SplitN(line, " ", 3); len(fields) == 3 && strings.Count(fields[0], "/") == 2 {
		line = fields[2]
	}
	if runes := []rune(line); len(runes) > detailRunes {
		line = string(runes[:detailRunes])
	}
	return line
}

func withoutRcloneVariables(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, pair := range environ {
		if !strings.HasPrefix(strings.ToUpper(pair), rcloneEnvPrefix) {
			kept = append(kept, pair)
		}
	}
	return kept
}

type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int64
	keepTail bool
	over     bool
}

func (c *cappedBuffer) Write(chunk []byte) (int, error) {
	if c.limit <= 0 {
		return len(chunk), nil
	}
	room := c.limit - int64(c.buf.Len())
	if int64(len(chunk)) <= room {
		c.buf.Write(chunk)
		return len(chunk), nil
	}
	c.over = true
	if c.keepTail {
		c.buf.Write(chunk)
		tail := append([]byte(nil), c.buf.Bytes()[c.buf.Len()-int(c.limit):]...)
		c.buf.Reset()
		c.buf.Write(tail)
		return len(chunk), nil
	}
	if room > 0 {
		c.buf.Write(chunk[:room])
	}
	return len(chunk), nil
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }

func (c *cappedBuffer) String() string { return c.buf.String() }
