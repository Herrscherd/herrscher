package host

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func stageAgentTar(sessionAgent agent.Agent, remoteWorktree, remoteBin string) (io.Reader, error) {
	stage, err := os.MkdirTemp("", "herrscher-agent-")
	if err != nil {
		return nil, wrapHost(err, "remote.stage_agent", map[string]any{nameParam: sessionAgent.Name})
	}
	defer func() { logCleanup(os.RemoveAll(stage)) }()
	if err := sessionAgent.MaterializeIntoAs(stage, remoteWorktree, remoteBin); err != nil {
		return nil, wrapHost(err, "remote.stage_agent", map[string]any{nameParam: sessionAgent.Name})
	}
	var buf bytes.Buffer
	if err := tarDir(stage, &buf); err != nil {
		return nil, wrapHost(err, "remote.pack_agent", map[string]any{nameParam: sessionAgent.Name})
	}
	return &buf, nil
}

func tarDir(root string, writer io.Writer) error {
	stage, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer closeRead(stage)
	tarWriter := tar.NewWriter(writer)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
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
		if writeErr := tarWriter.WriteHeader(hdr); writeErr != nil {
			return writeErr
		}
		file, err := stage.Open(rel)
		if err != nil {
			return err
		}
		defer closeRead(file)
		_, err = io.Copy(tarWriter, file)
		return err
	})
	if err != nil {
		return err
	}
	return tarWriter.Close()
}

func extractTar(src io.Reader, dst string) error {
	if err := os.MkdirAll(dst, privateDirMode); err != nil {
		return err
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer closeRead(root)
	tarReader := tar.NewReader(src)
	for {
		hdr, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if err := extractRegularMember(tarReader, hdr, root); err != nil {
			return err
		}
	}
}

func extractRegularMember(reader *tar.Reader, hdr *tar.Header, root *os.Root) error {
	name := filepath.FromSlash(hdr.Name)
	if !filepath.IsLocal(name) {
		return &unsafeTarMemberError{Member: hdr.Name, Root: root.Name()}
	}
	if mkdirErr := root.MkdirAll(filepath.Dir(name), privateDirMode); mkdirErr != nil {
		return mkdirErr
	}
	return writeTarMember(reader, hdr, root, name)
}

func writeTarMember(reader *tar.Reader, hdr *tar.Header, root *os.Root, name string) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, hdr.FileInfo().Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.CopyN(file, reader, hdr.Size); err != nil {
		logCleanup(file.Close())
		return err
	}
	return file.Close()
}
