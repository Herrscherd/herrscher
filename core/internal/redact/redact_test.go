package redact

import (
	"strings"
	"testing"
)

func TestOutputMasksSecrets(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantAbsent string
		wantKeep   string
	}{
		{"url userinfo", "fatal: unable to access https://alice:s3cr3t@github.com/x.git", "s3cr3t", "github.com"},
		{"gh classic token", "error: bad credentials ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", "bad credentials"},
		{"gh fine-grained pat", "token github_pat_11ABCDEFG0abcdefghijklmnop rejected", "github_pat_11ABCDEFG0abcdefghijklmnop", "rejected"},
		{"gitlab pat", "401 with glpat-ABCDEFGHIJ1234567890", "glpat-ABCDEFGHIJ1234567890", "401"},
		{"bearer header", "Authorization: Bearer eyJhbGciOi.foo.bar", "eyJhbGciOi.foo.bar", "Bearer"},
		{"password field", "url?token=abc123&password=hunter2 nope", "hunter2", "nope"},
		{"api_key query", "GET https://h.test/v1?api_key=XYZ123&a=1 failed", "XYZ123", "failed"},
		{"apikey query, any case", "url ?APIKEY=ZZZ9 failed", "ZZZ9", "failed"},
		{"key query", "https://h.test/x?key=KKK99 bad", "KKK99", "bad"},
		{"env variable", "OPENAI_API_KEY=sk-live-ABC123 rejected", "sk-live-ABC123", "OPENAI_API_KEY="},
		{"snake case key", "x_api_key=ZZ1 rejected", "ZZ1", "rejected"},
		{"x-api-key header", "request headers: X-Api-Key: HDR123 refused", "HDR123", "refused"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Output([]byte(c.in))
			if strings.Contains(got, c.wantAbsent) {
				t.Fatalf("secret leaked: %q still contains %q", got, c.wantAbsent)
			}
			if !strings.Contains(got, mask) {
				t.Fatalf("expected a %q marker in %q", mask, got)
			}
			if c.wantKeep != "" && !strings.Contains(got, c.wantKeep) {
				t.Fatalf("diagnostic context %q lost from %q", c.wantKeep, got)
			}
		})
	}
}

func TestOutputLeavesNamesThatOnlyEndInKey(t *testing.T) {
	got := Output([]byte("monkey=banana turkey=dinde"))
	if got != "monkey=banana turkey=dinde" {
		t.Fatalf("unrelated pairs altered: %q", got)
	}
}

func TestOutputTrimsAndPreservesPlainText(t *testing.T) {
	got := Output([]byte("  merge conflict in main.go\n"))
	if got != "merge conflict in main.go" {
		t.Fatalf("plain text altered: %q", got)
	}
}

func TestOutputCapsLength(t *testing.T) {
	got := Output([]byte(strings.Repeat("x", maxOutputRunes+500)))
	if !strings.HasSuffix(got, "… (truncated)") {
		t.Fatalf("expected truncation marker, got tail %q", got[len(got)-20:])
	}
	if n := len([]rune(got)); n > maxOutputRunes+len([]rune("… (truncated)")) {
		t.Fatalf("output not capped: %d runes", n)
	}
}
