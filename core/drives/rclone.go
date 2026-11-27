package drives

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

const (
	defaultRclone      = "rclone"
	rcloneEnvPrefix    = "RCLONE_"
	listOutputBytes    = 8 << 20
	statOutputBytes    = 64 << 10
	stderrBytes        = 4 << 10
	detailRunes        = 300
	exitDirNotFound    = 3
	exitFileNotFound   = 4
	rcloneWaitDelay    = 5 * time.Second
	versionOutputBytes = 4 << 10
	logPrefixFields    = 3
	logDateSlashes     = 2
	rcloneLockSuffix   = ".lock"
)

var (
	// ErrNoSuchFile reports a path the remote does not hold.
	ErrNoSuchFile = errors.New("no such file or folder on the drive")
	// ErrIsFolder reports a read of a path that is a folder.
	ErrIsFolder = errors.New("the path is a folder: list it with drive ls")
)

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
// setup never leaks in. A run that may refresh a token holds a shared lock
// beside the configuration, and a run that rewrites it on purpose holds that
// lock alone, so a sign in or a removal never races a refresh.
type Rclone struct {
	argv   []string
	config string
	env    []string
}

// NewRclone builds the runner. command is split on spaces, and an empty one runs rclone from PATH.
func NewRclone(command, config string) Rclone {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		fields = []string{defaultRclone}
	}
	return Rclone{argv: fields, config: config}
}

// With is the runner with env added to the environment of each rclone it starts.
func (r Rclone) With(env []string) Rclone {
	r.env = append(append([]string(nil), r.env...), env...)
	return r
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

// List lists one level of target, at most limit items. It reports whether more were left out.
func (r Rclone) List(ctx context.Context, target string, limit int) ([]Item, bool, error) {
	var items []Item
	truncated := false
	err := r.stream(ctx, "lsjson", func(out io.Reader) (bool, error) {
		counted := &countingReader{reader: io.LimitReader(out, listOutputBytes)}
		decoded, more, decodeErr := decodeItems(json.NewDecoder(counted), limit)
		items = decoded
		if decodeErr != nil && counted.read >= listOutputBytes {
			truncated = true
			return true, nil
		}
		truncated = more
		return more, decodeErr
	}, "lsjson", "--max-depth", "1", "--no-mimetype", "--", target)
	if err != nil {
		return nil, false, err
	}
	return items, truncated, nil
}

func decodeItems(decoder *json.Decoder, limit int) ([]Item, bool, error) {
	if _, err := decoder.Token(); err != nil {
		return nil, false, err
	}
	var items []Item
	for decoder.More() {
		if len(items) == limit {
			return items, true, nil
		}
		var item Item
		if err := decoder.Decode(&item); err != nil {
			return items, false, err
		}
		items = append(items, item)
	}
	return items, false, nil
}

// Stat describes target.
func (r Rclone) Stat(ctx context.Context, target string) (Item, error) {
	var item Item
	err := r.stream(ctx, "lsjson", func(out io.Reader) (bool, error) {
		return false, json.NewDecoder(io.LimitReader(out, statOutputBytes)).Decode(&item)
	}, "lsjson", "--stat", "--no-mimetype", "--", target)
	return item, err
}

// Read copies the content of target to dst, refused past limit bytes.
func (r Rclone) Read(ctx context.Context, target string, dst io.Writer, limit int64) (int64, error) {
	var copied int64
	err := r.stream(ctx, "cat", func(out io.Reader) (bool, error) {
		written, copyErr := io.Copy(dst, io.LimitReader(out, limit+1))
		copied = written
		if copyErr != nil {
			return true, copyErr
		}
		if written > limit {
			return true, &TooLargeError{Limit: limit}
		}
		return false, nil
	}, "cat", "--count", strconv.FormatInt(limit+1, 10), "--", target)
	return copied, err
}

// Write stores size bytes read from body at target.
func (r Rclone) Write(ctx context.Context, target string, body io.Reader, size int64) error {
	release, err := r.shared(ctx)
	if err != nil {
		return err
	}
	defer release()
	cmd := r.prepare(ctx, []string{"--ask-password=false", "rcat", "--size", strconv.FormatInt(size, 10), "--", target})
	cmd.Stdin = body
	errOut := &tailBuffer{limit: stderrBytes}
	cmd.Stdout, cmd.Stderr = io.Discard, errOut
	if runErr := cmd.Run(); runErr != nil {
		return rcloneFailure("rcat", runErr, errOut.String())
	}
	return nil
}

// Version is the first line of rclone version.
func (r Rclone) Version(ctx context.Context) (string, error) {
	cmd := r.prepare(ctx, []string{"version"})
	out := &tailBuffer{limit: versionOutputBytes}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(nil), out, io.Discard
	if err := cmd.Run(); err != nil {
		return "", rcloneFailure("version", err, "")
	}
	first, _, _ := strings.Cut(out.String(), "\n")
	return strings.TrimSpace(first), nil
}

// DeleteRemote removes the remote name from the configuration, once no other rclone runs on it.
func (r Rclone) DeleteRemote(ctx context.Context, name string) error {
	release, err := r.exclusive(ctx)
	if err != nil {
		return err
	}
	defer release()
	cmd := r.prepare(ctx, []string{"--ask-password=false", "config", "delete", name})
	errOut := &tailBuffer{limit: stderrBytes}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(nil), io.Discard, errOut
	if runErr := cmd.Run(); runErr != nil {
		return rcloneFailure("config delete", runErr, errOut.String())
	}
	return nil
}

// Terminal is the standard streams an interactive rclone run is attached to.
type Terminal struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// CreateRemote runs rclone config create in the terminal, which signs in
// through the browser for an OAuth kind. The other runs wait until it ends.
func (r Rclone) CreateRemote(ctx context.Context, name, rcloneType string, params []string, term Terminal) error {
	release, err := r.exclusive(ctx)
	if err != nil {
		return err
	}
	defer release()
	cmd := r.prepare(ctx, append([]string{"config", "create", name, rcloneType}, params...))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = term.In, term.Out, term.Err
	if runErr := cmd.Run(); runErr != nil {
		return &RcloneError{Op: "config create", Code: exitCode(runErr), Cause: runErr}
	}
	return nil
}

func (r Rclone) shared(ctx context.Context) (func(), error) {
	if err := ensureDir(r.config); err != nil {
		return nil, err
	}
	return filelock.AcquireShared(ctx, r.config+rcloneLockSuffix)
}

func (r Rclone) exclusive(ctx context.Context) (func(), error) {
	if err := ensureDir(r.config); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, r.config+rcloneLockSuffix)
}

func (r Rclone) prepare(ctx context.Context, args []string) *exec.Cmd {
	argv := append(append(append([]string{}, r.argv[1:]...), "--config", r.config), args...)
	cmd := exec.CommandContext(ctx, r.argv[0], argv...)
	cmd.Env = append(withoutRcloneVariables(os.Environ()), r.env...)
	cmd.WaitDelay = rcloneWaitDelay
	return cmd
}

func (r Rclone) stream(ctx context.Context, operation string, consume func(io.Reader) (bool, error), args ...string) error {
	release, err := r.shared(ctx)
	if err != nil {
		return err
	}
	defer release()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := r.prepare(runCtx, append([]string{"--ask-password=false"}, args...))
	cmd.Stdin = bytes.NewReader(nil)
	errOut := &tailBuffer{limit: stderrBytes}
	cmd.Stderr = errOut
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return &RcloneError{Op: operation, Cause: err}
	}
	stopped, consumeErr := consume(out)
	if stopped {
		cancel()
		reapStopped(cmd)
		return consumeErr
	}
	_, drainErr := io.Copy(io.Discard, out)
	if waitErr := cmd.Wait(); waitErr != nil {
		return rcloneFailure(operation, waitErr, errOut.String())
	}
	if consumeErr != nil {
		return &RcloneError{Op: operation, Cause: consumeErr}
	}
	return drainErr
}

func reapStopped(cmd *exec.Cmd) {
	if err := cmd.Wait(); err != nil {
		slog.Debug("rclone stopped early", "err", err)
	}
}

func rcloneFailure(operation string, runErr error, stderr string) error {
	code := exitCode(runErr)
	failure := &RcloneError{Op: operation, Code: code, Detail: lastLine(stderr), Cause: runErr}
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
	} else if fields := strings.SplitN(line, " ", logPrefixFields); len(fields) == logPrefixFields && strings.Count(fields[0], "/") == logDateSlashes {
		line = fields[logPrefixFields-1]
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

type countingReader struct {
	reader io.Reader
	read   int64
}

func (c *countingReader) Read(chunk []byte) (int, error) {
	count, err := c.reader.Read(chunk)
	c.read += int64(count)
	return count, err
}

type tailBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (t *tailBuffer) Write(chunk []byte) (int, error) {
	t.buf.Write(chunk)
	if extra := t.buf.Len() - t.limit; extra > 0 {
		tail := append([]byte(nil), t.buf.Bytes()[extra:]...)
		t.buf.Reset()
		t.buf.Write(tail)
	}
	return len(chunk), nil
}

func (t *tailBuffer) String() string { return t.buf.String() }
