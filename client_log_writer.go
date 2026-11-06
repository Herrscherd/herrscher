package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

var (
	errNonPositiveLimit = errors.New("client log cap must be positive")
	errRotationStalled  = errors.New("client log rotation left the current file at its cap")
)

type rotatingWriter struct {
	path  string
	limit int64
	file  *os.File
	size  int64
}

func openRotatingWriter(path string, limit int64) (*rotatingWriter, error) {
	if limit <= 0 {
		return nil, errNonPositiveLimit
	}
	writer := &rotatingWriter{path: path, limit: limit}
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *rotatingWriter) open() error {
	file, err := os.OpenFile(filepath.Clean(w.path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, modeSecret)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close())
	}
	w.file, w.size = file, info.Size()
	return nil
}

func (w *rotatingWriter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		if w.size >= w.limit {
			if err := w.rotate(); err != nil {
				return written, err
			}
			if w.size >= w.limit {
				return written, errRotationStalled
			}
		}
		chunk := data[:min(int64(len(data)), w.limit-w.size)]
		count, err := w.file.Write(chunk)
		w.size += int64(count)
		written += count
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, io.ErrShortWrite
		}
		data = data[count:]
	}
	return written, nil
}

func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return errors.Join(err, w.open())
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		return errors.Join(err, w.open())
	}
	if err := os.Chmod(w.path+".1", modeSecret); err != nil {
		return errors.Join(err, w.open())
	}
	return w.open()
}

func (w *rotatingWriter) Close() error {
	return w.file.Close()
}
