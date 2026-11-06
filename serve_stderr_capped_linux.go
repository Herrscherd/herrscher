//go:build linux

package main

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func redirectStderrCapped(path string, limit int64) (func() error, error) {
	sink, err := openRotatingWriter(path, limit)
	if err != nil {
		return nil, err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, errors.Join(err, sink.Close())
	}
	stderrFd := int(os.Stderr.Fd())
	saved, err := unix.Dup(stderrFd)
	if err != nil {
		return nil, errors.Join(err, reader.Close(), writer.Close(), sink.Close())
	}
	if err := unix.Dup3(int(writer.Fd()), stderrFd, 0); err != nil {
		return nil, errors.Join(err, unix.Close(saved), reader.Close(), writer.Close(), sink.Close())
	}
	pumped := make(chan error, 1)
	go func() { pumped <- pumpStderr(reader, sink) }()
	return func() error {
		if err := unix.Dup3(saved, stderrFd, 0); err != nil {
			return errors.Join(err, unix.Close(saved), writer.Close(), sink.Close())
		}
		return errors.Join(unix.Close(saved), writer.Close(), <-pumped, sink.Close())
	}, nil
}

func pumpStderr(source io.ReadCloser, sink io.Writer) error {
	_, copyErr := io.Copy(sink, source)
	if copyErr == nil {
		return source.Close()
	}
	_, drainErr := io.Copy(io.Discard, source)
	return errors.Join(copyErr, drainErr, source.Close())
}
