package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/outbox"
)

func loadOutboundConfig(t *testing.T, megabytes string) (Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"outboundFileMaxMB": `+megabytes+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestLoadRefusesOutboundFileMaxMBOutOfRange(t *testing.T) {
	cases := []struct {
		name      string
		megabytes string
		wantErr   error
	}{
		{name: "negative is refused", megabytes: "-1", wantErr: errOutboundFileNegative},
		{name: "above the maximum is refused", megabytes: "1025", wantErr: errOutboundFileTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadOutboundConfig(t, tc.megabytes); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Load error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestEffectiveOutboundMaxBytes(t *testing.T) {
	cases := []struct {
		name      string
		megabytes string
		wantBytes int64
	}{
		{name: "zero keeps the default", megabytes: "0", wantBytes: outbox.DefaultMaxBytes},
		{name: "valid size gives its bytes", megabytes: "5", wantBytes: 5 << 20},
		{name: "maximum is accepted", megabytes: "1024", wantBytes: 1024 << 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadOutboundConfig(t, tc.megabytes)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.EffectiveOutboundMaxBytes(); got != tc.wantBytes {
				t.Fatalf("EffectiveOutboundMaxBytes = %d, want %d", got, tc.wantBytes)
			}
		})
	}
}
