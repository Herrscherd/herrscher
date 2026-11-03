package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
)

func loadEnvFile(path string) error {
	defer hideGatewayKeys()
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open env file: %w", err)
	}
	return errors.Join(scanEnvFile(file), file.Close())
}

func scanEnvFile(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, envLineInitial), envLineLimit)
	for scanner.Scan() {
		key, val, ok := parseEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, unquoteEnv(val)); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return scanner.Err()
}

func parseEnvLine(raw string) (string, string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	if rest, isExport := strings.CutPrefix(line, "export"); isExport && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		line = strings.TrimSpace(rest)
	}
	key, val, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	return key, strings.TrimSpace(val), found && key != ""
}

func hideGatewayKeys() { host.CaptureGatewayConfig(contracts.Default.Gateways()) }

func unquoteEnv(raw string) string {
	if len(raw) >= quotedMinLen {
		if c := raw[0]; (c == '"' || c == '\'') && raw[len(raw)-1] == c {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}
