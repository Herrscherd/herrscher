package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
)

const (
	knownHostFields = 3
	scratchDirPerm  = 0o700
	pinnedFilePerm  = 0o600
)

// ErrHostKeyUnreadable reports that no host key could be learned from the target.
var ErrHostKeyUnreadable = errors.New("the ssh host key could not be read")

// ErrBinaryChanged reports a herrscher binary whose digest is not the recorded one.
var ErrBinaryChanged = errors.New("the herrscher binary on the host is not the one recorded")

// ErrDigestUnreadable reports a digest command whose output is not a SHA256.
var ErrDigestUnreadable = errors.New("the binary digest could not be read")

// ErrLoadUnreadable reports a load average command whose output has no number.
var ErrLoadUnreadable = errors.New("the load average could not be read")

// ErrKnownHostsPath reports a known_hosts path that ssh options cannot carry.
var ErrKnownHostsPath = errors.New("the known_hosts path cannot be quoted for ssh")

// HostKey is the public key an ssh server presents, kept as the line a known_hosts file holds and as its SHA256 fingerprint.
type HostKey struct {
	Line        string
	Fingerprint string
}

// ParseHostKey reads a known_hosts line and fingerprints its key the way ssh-keygen -l does.
func ParseHostKey(line string) (HostKey, error) {
	fields := strings.Fields(line)
	if len(fields) < knownHostFields {
		return HostKey{}, fmt.Errorf("%w: %q", ErrHostKeyUnreadable, line)
	}
	blob, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		return HostKey{}, fmt.Errorf("%w: %w", ErrHostKeyUnreadable, err)
	}
	sum := sha256.Sum256(blob)
	return HostKey{Line: strings.Join(fields[:3], " "), Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])}, nil
}

// ScanHostKey connects to target once and returns the key its server presented. The connection learns the key into a
// throwaway file, never into the operator's known_hosts, and the login that follows is allowed to fail: the key is
// already known by then.
func ScanHostKey(ctx context.Context, target string) (HostKey, error) {
	if err := CheckTarget(target); err != nil {
		return HostKey{}, err
	}
	dir, err := os.MkdirTemp("", "herrscher-hostkey-")
	if err != nil {
		return HostKey{}, err
	}
	defer removeScratch(dir)
	learned := filepath.Join(dir, "known_hosts")
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=\""+learned+"\"",
		"-o", "GlobalKnownHostsFile="+os.DevNull,
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"--", target, "true")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	data, readErr := fs.ReadFile(os.DirFS(dir), filepath.Base(learned))
	if readErr != nil {
		return HostKey{}, fmt.Errorf("%w: %s", ErrHostKeyUnreadable, failureText(stderr.String(), runErr))
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			return ParseHostKey(line)
		}
	}
	return HostKey{}, fmt.Errorf("%w: %s", ErrHostKeyUnreadable, failureText(stderr.String(), runErr))
}

func removeScratch(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("remove scratch directory", "dir", dir, "err", err)
	}
}

func failureText(stderr string, runErr error) string {
	if msg := strings.TrimSpace(stderr); msg != "" {
		return msg
	}
	if runErr != nil {
		return runErr.Error()
	}
	return "no key was presented"
}

// PinHostKey writes key as the only line of the known_hosts file at path, replacing whatever it held.
func PinHostKey(path string, key HostKey) error {
	if err := CheckKnownHostsPath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), scratchDirPerm); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(key.Line+"\n"), pinnedFilePerm)
}

// CheckKnownHostsPath rejects a path that cannot sit between the quotes of an ssh option.
func CheckKnownHostsPath(path string) error {
	if strings.ContainsAny(path, "\"\n\r") {
		return fmt.Errorf("%w: %q", ErrKnownHostsPath, path)
	}
	return nil
}

const binaryDigestScript = `if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi`

// BinaryDigestArgv is the command that prints the SHA256 of bin on a machine, whichever digest tool it has.
func BinaryDigestArgv(bin string) []string {
	return []string{"sh", "-c", binaryDigestScript, "sh", bin}
}

// ParseDigest reads the hex digest that a sha256sum or shasum line starts with.
func ParseDigest(out string) (string, error) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", ErrDigestUnreadable
	}
	if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != sha256.Size*2 {
		return "", fmt.Errorf("%w: %q", ErrDigestUnreadable, out)
	}
	return strings.ToLower(fields[0]), nil
}

// FileSHA256 digests a local file, the value a copied binary must still match.
func FileSHA256(path string) (string, error) {
	data, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// BinaryDigest returns the SHA256 of bin on the target.
func (s SSH) BinaryDigest(ctx context.Context, bin string) (string, error) {
	out, err := s.Command(ctx, "", BinaryDigestArgv(bin)...).Output()
	if err != nil {
		return "", fmt.Errorf("host %q: %w", s.Target, commandError(err))
	}
	return ParseDigest(string(out))
}

// VerifyBinary refuses a binary on the target whose digest is not want.
func (s SSH) VerifyBinary(ctx context.Context, bin, want string) error {
	got, err := s.BinaryDigest(ctx, bin)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%w: %s on %s is %s, recorded %s", ErrBinaryChanged, bin, s.Target, got, want)
	}
	return nil
}

// LoadArgv is the command that prints the 1-minute load average on Linux or macOS.
func LoadArgv() []string {
	return []string{"sh", "-c", "cat /proc/loadavg 2>/dev/null || sysctl -n vm.loadavg"}
}

// ParseLoad reads the 1-minute load average from the output of LoadArgv.
func ParseLoad(out string) (float64, error) {
	cleaned := strings.NewReplacer("{", " ", "}", " ").Replace(out)
	fields := strings.Fields(cleaned)
	if len(fields) == 0 {
		return 0, ErrLoadUnreadable
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrLoadUnreadable, out)
	}
	return load, nil
}
