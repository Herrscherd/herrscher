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

func stageAgentTar(a agent.Agent, remoteWorktree, remoteBin string) (io.Reader, error) {
	stage, err := os.MkdirTemp("", "herrscher-agent-")
	if err != nil {
		return nil, fmt.Errorf("stage agent %q: %w", a.Name, err)
	}
	defer func() { logCleanup(os.RemoveAll(stage)) }()
	if err := a.MaterializeIntoAs(stage, remoteWorktree, remoteBin); err != nil {
		return nil, fmt.Errorf("stage agent %q: %w", a.Name, err)
	}
	var buf bytes.Buffer
	if err := tarDir(stage, &buf); err != nil {
		return nil, fmt.Errorf("pack agent %q: %w", a.Name, err)
	}
	return &buf, nil
}

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
		defer closeRead(f)
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

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
			logCleanup(f.Close())
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
}
