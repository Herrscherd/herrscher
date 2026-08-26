package host

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// stageAgentTar renders an agent's provisioning files here, for a worktree over
// there, and returns them as one tar stream.
//
// The files are small and there are six of them, so one round trip carrying all
// of them beats six copies. They are rendered locally because the agent's home
// is local: only the path written INTO them belongs to the far machine, which is
// why MaterializeIntoAs takes the destination and that path separately.
//
// remoteBin is herrscher over there, the binary the approval hook rendered into
// the settings must invoke; empty means the session wants no hook.
func stageAgentTar(a agent.Agent, remoteWorktree, remoteBin string) (io.Reader, error) {
	stage, err := os.MkdirTemp("", "herrscher-agent-")
	if err != nil {
		return nil, fmt.Errorf("stage agent %q: %w", a.Name, err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := a.MaterializeIntoAs(stage, remoteWorktree, remoteBin); err != nil {
		return nil, fmt.Errorf("stage agent %q: %w", a.Name, err)
	}
	var buf bytes.Buffer
	if err := tarDir(stage, &buf); err != nil {
		return nil, fmt.Errorf("pack agent %q: %w", a.Name, err)
	}
	return &buf, nil
}

// tarDir writes root's regular files into w, named relative to root.
func tarDir(root string, w io.Writer) error {
	tw := tar.NewWriter(w)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:     filepath.ToSlash(rel),
			Mode:     int64(info.Mode().Perm()),
			Size:     info.Size(),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// extractTar writes a tar stream into dst. Every member is checked against dst
// before a byte is written: a tar names its own destinations, and this one
// arrives over a pipe.
func extractTar(r io.Reader, dst string) error {
	root, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			// Only regular files are ever packed, so anything else is not from
			// stageAgentTar and has no business being written.
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(hdr.Name))
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("refusing tar member %q: it lands outside %s", hdr.Name, root)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(hdr.Mode).Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
}
