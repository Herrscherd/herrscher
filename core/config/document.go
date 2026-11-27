package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	documentMode = 0o600
	dirMode      = 0o700
	backupSuffix = ".bak"
	keySeparator = "."
)

var (
	// ErrEmptyKey reports a dotted key with an empty segment.
	ErrEmptyKey = errors.New("config: a key needs a name in every segment")
	// ErrNoSuchKey reports a dotted key the document does not hold.
	ErrNoSuchKey = errors.New("config: no such key")
	// ErrNotAnObject reports a dotted key that crosses a value which is not an object.
	ErrNotAnObject = errors.New("config: the key crosses a value that is not an object")
	// ErrUnknownKey reports a key the daemon does not read, or reads under another spelling.
	ErrUnknownKey = errors.New("config: the daemon reads no such key")
)

// Document is config.json as a tree of JSON values, the way an editor sees it rather than the way the daemon reads it.
type Document struct {
	root     map[string]any
	comments bool
	edited   []string
}

// ReadDocument reads the config.json at path. A missing file is an empty document.
func ReadDocument(path string) (Document, error) {
	buf, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return Document{root: make(map[string]any)}, nil
	}
	if err != nil {
		return Document{}, fmt.Errorf("read %s: %w", path, err)
	}
	stripped := stripComments(buf)
	root := make(map[string]any)
	if len(bytes.TrimSpace(stripped)) > 0 {
		if err := json.Unmarshal(stripped, &root); err != nil {
			return Document{}, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	return Document{root: root, comments: !bytes.Equal(bytes.TrimSpace(stripped), bytes.TrimSpace(buf))}, nil
}

func splitKey(key string) ([]string, error) {
	segments := strings.Split(strings.TrimSpace(key), keySeparator)
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return nil, fmt.Errorf("%w: %q", ErrEmptyKey, key)
		}
	}
	return segments, nil
}

// Get returns the value at a dotted key, or the whole document for an empty key, as indented JSON.
func (d *Document) Get(key string) (string, error) {
	var value any = d.root
	if strings.TrimSpace(key) != "" {
		segments, err := splitKey(key)
		if err != nil {
			return "", err
		}
		value, err = lookup(d.root, segments, key)
		if err != nil {
			return "", err
		}
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("config: encode %q: %w", key, err)
	}
	return string(out), nil
}

func lookup(root map[string]any, segments []string, key string) (any, error) {
	var current any = root
	for _, segment := range segments {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrNotAnObject, key)
		}
		current, ok = object[segment]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrNoSuchKey, key)
		}
	}
	return current, nil
}

// Set puts value at a dotted key, creating the objects on its way. A value that is valid JSON is stored as that JSON, anything else as a string.
func (d *Document) Set(key, value string) error {
	segments, err := splitKey(key)
	if err != nil {
		return err
	}
	parent, err := parentOf(d.root, segments, key, true)
	if err != nil {
		return err
	}
	parent[segments[len(segments)-1]] = parseValue(value)
	d.edited = append(d.edited, key)
	return nil
}

// Unset removes a dotted key.
func (d *Document) Unset(key string) error {
	segments, err := splitKey(key)
	if err != nil {
		return err
	}
	parent, err := parentOf(d.root, segments, key, false)
	if err != nil {
		return err
	}
	last := segments[len(segments)-1]
	if _, ok := parent[last]; !ok {
		return fmt.Errorf("%w: %q", ErrNoSuchKey, key)
	}
	delete(parent, last)
	return nil
}

func parentOf(root map[string]any, segments []string, key string, create bool) (map[string]any, error) {
	current := root
	for _, segment := range segments[:len(segments)-1] {
		next, present := current[segment]
		if !present {
			if !create {
				return nil, fmt.Errorf("%w: %q", ErrNoSuchKey, key)
			}
			made := make(map[string]any)
			current[segment] = made
			current = made
			continue
		}
		object, ok := next.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrNotAnObject, key)
		}
		current = object
	}
	return current, nil
}

func parseValue(value string) any {
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err == nil {
		return parsed
	}
	return value
}

// Validate refuses a document the daemon would refuse to start with, read the way Load reads it, and a key set since it was read that the daemon does not read under that exact spelling.
func (d *Document) Validate() error {
	raw, err := json.Marshal(d.root)
	if err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := validateLoaded(cfg); err != nil {
		return err
	}
	for _, key := range d.edited {
		if err := d.checkKnown(key); err != nil {
			return err
		}
	}
	return nil
}

func (d *Document) checkKnown(key string) error {
	segments, err := splitKey(key)
	if err != nil {
		return err
	}
	value, err := lookup(d.root, segments, key)
	if err != nil {
		return err
	}
	alone := make(map[string]any)
	parent := alone
	for _, segment := range segments[:len(segments)-1] {
		child := make(map[string]any)
		parent[segment] = child
		parent = child
	}
	parent[segments[len(segments)-1]] = value
	raw, err := json.Marshal(alone)
	if err != nil {
		return fmt.Errorf("config: encode %q: %w", key, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return fmt.Errorf("%w: %q: %w", ErrUnknownKey, key, err)
	}
	return spelledAsRead(cfg, segments, key)
}

func spelledAsRead(cfg Config, segments []string, key string) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: encode %q: %w", key, err)
	}
	var read map[string]any
	if err := json.Unmarshal(raw, &read); err != nil {
		return fmt.Errorf("config: decode %q: %w", key, err)
	}
	current := read
	for _, segment := range segments {
		if _, exact := current[segment]; exact {
			next, isObject := current[segment].(map[string]any)
			if !isObject {
				return nil
			}
			current = next
			continue
		}
		for spelled := range current {
			if strings.EqualFold(spelled, segment) {
				return fmt.Errorf("%w: %q, the daemon reads %q", ErrUnknownKey, key, spelled)
			}
		}
		return nil
	}
	return nil
}

// Write validates the document and writes it to path. When the file held comments, which a rewrite cannot place again, the previous file is kept beside it and its path returned.
func (d *Document) Write(path string) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(d.root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("config: encode: %w", err)
	}
	backup := ""
	if d.comments {
		backup = path + backupSuffix
		previous, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return "", fmt.Errorf("read %s: %w", path, readErr)
		}
		if err := os.WriteFile(backup, previous, documentMode); err != nil {
			return "", fmt.Errorf("write %s: %w", backup, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, append(out, '\n'), documentMode); err != nil {
		return "", fmt.Errorf("write %s: %w", temp, err)
	}
	if err := os.Rename(temp, path); err != nil {
		return "", fmt.Errorf("replace %s: %w", path, err)
	}
	return backup, nil
}
