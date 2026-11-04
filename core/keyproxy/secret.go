package keyproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/config"
)

const (
	keyFileForbiddenBits  fs.FileMode = 0o077
	keyCommandTimeout                 = 30 * time.Second
	keyCommandWaitDelay               = 2 * time.Second
	keyCommandOutputBytes             = 16 << 10
)

var errKeyOutputTooLarge = errors.New("key command printed more than its output cap")

var (
	// ErrKeyFileOpen reports a key file readable or writable by anyone but its owner.
	ErrKeyFileOpen       = errors.New("key file is readable by others than its owner, chmod 600 it")
	errKeyFileNotRegular = errors.New("key file must be a regular file, not a link, pipe or device")
	errKeyDirUnsafe      = errors.New("key file directory must belong to the user or root, and must not be writable by its group or others")
	// ErrKeyEmpty reports a key source that yields a blank key.
	ErrKeyEmpty = errors.New("key source yields an empty key")
	// ErrKeyCommand reports a key_cmd that failed; its output is never included.
	ErrKeyCommand = errors.New("key command failed")
)

// Secret is one API key, named by its source in the config. Its value never prints.
type Secret struct {
	name  string
	value string
}

// NewSecret wraps a key obtained in code, such as the gateway token, under a name for logs.
func NewSecret(name, value string) Secret {
	return Secret{name: name, value: value}
}

// Name returns the key name given in the config.
func (s Secret) Name() string { return s.name }

// Value returns the key itself; only the forwarding code reads it.
func (s Secret) Value() string { return s.value }

// String prints the key name, never its value.
func (s Secret) String() string {
	return "key " + s.name
}

// GoString prints the key name, never its value, for %#v.
func (s Secret) GoString() string {
	return "keyproxy.Secret(" + s.name + ")"
}

// Loader reads key sources; its fields are replaceable in tests.
type Loader struct {
	Getenv   func(string) string
	Lstat    func(string) (fs.FileInfo, error)
	OpenFile func(string) (*os.File, error)
	Run      func(ctx context.Context, argv []string) ([]byte, error)
	Home     func() (string, error)
}

// OSLoader returns the loader that reads the real environment, files and commands.
func OSLoader() Loader {
	return Loader{
		Getenv:   os.Getenv,
		Lstat:    os.Lstat,
		OpenFile: openKeyFile,
		Run:      runKeyCommand,
		Home:     os.UserHomeDir,
	}
}

func runKeyCommand(ctx context.Context, argv []string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, keyCommandTimeout)
	defer cancel()
	command := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	command.WaitDelay = keyCommandWaitDelay
	output := &boundedOutput{limit: keyCommandOutputBytes}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	return output.buf.Bytes(), nil
}

type boundedOutput struct {
	buf   bytes.Buffer
	limit int
}

func (o *boundedOutput) Write(chunk []byte) (int, error) {
	if o.buf.Len()+len(chunk) > o.limit {
		return 0, errKeyOutputTooLarge
	}
	return o.buf.Write(chunk)
}

// Load reads the key a source names and rejects a blank one.
func (l Loader) Load(ctx context.Context, source config.KeySource) (Secret, error) {
	value, err := l.value(ctx, source)
	if err != nil {
		return Secret{}, fmt.Errorf("key %q: %w", source.Name, err)
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Secret{}, fmt.Errorf("key %q: %w", source.Name, ErrKeyEmpty)
	}
	return Secret{name: source.Name, value: trimmed}, nil
}

func (l Loader) value(ctx context.Context, source config.KeySource) (string, error) {
	switch {
	case source.KeyEnv != "":
		return l.Getenv(source.KeyEnv), nil
	case source.KeyFile != "":
		return l.fromFile(source.KeyFile)
	case len(source.KeyCmd) > 0:
		return l.fromCommand(ctx, source.KeyCmd)
	}
	return "", config.ErrKeysSource
}

func (l Loader) fromFile(path string) (string, error) {
	resolved, err := l.expandHome(path)
	if err != nil {
		return "", err
	}
	if dirErr := l.checkKeyDirectories(resolved); dirErr != nil {
		return "", dirErr
	}
	file, openErr := l.OpenFile(resolved)
	if openErr != nil {
		return "", fmt.Errorf("open key file: %w", openErr)
	}
	value, readErr := readOpenKey(file)
	closeErr := file.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", fmt.Errorf("close key file: %w", closeErr)
	}
	return value, nil
}

func (l Loader) checkKeyDirectories(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve key file path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return fmt.Errorf("resolve key file directory: %w", err)
	}
	for dir := resolved; ; {
		info, statErr := l.Lstat(dir)
		if statErr != nil {
			return fmt.Errorf("stat key file directory: %w", statErr)
		}
		if dirErr := checkKeyDirectory(dir, info, int64(os.Getuid())); dirErr != nil {
			return dirErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func readOpenKey(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat key file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s: %w", filepath.Base(file.Name()), errKeyFileNotRegular)
	}
	if info.Mode().Perm()&keyFileForbiddenBits != 0 {
		return "", fmt.Errorf("%s: %w", filepath.Base(file.Name()), ErrKeyFileOpen)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("read key file: %w", err)
	}
	return string(data), nil
}

func (l Loader) expandHome(path string) (string, error) {
	rest, underHome := strings.CutPrefix(path, "~/")
	if !underHome {
		return path, nil
	}
	home, err := l.Home()
	if err != nil {
		return "", fmt.Errorf("resolve home for key file: %w", err)
	}
	return filepath.Join(home, rest), nil
}

func (l Loader) fromCommand(ctx context.Context, argv []string) (string, error) {
	out, err := l.Run(ctx, argv)
	if err != nil {
		return "", fmt.Errorf("%s: %w", argv[0], ErrKeyCommand)
	}
	return string(out), nil
}
