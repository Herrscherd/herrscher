package instanceid

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		id   string
		ok   bool
	}{
		{"simple", "alice", true},
		{"with-digits", "u12345678", true},
		{"with-hyphen", "team-a", true},
		{"max-len-16", "abcdefghijklmnop", true},
		{"single-char", "a", true},
		{"empty", "", false},
		{"too-long-17", "abcdefghijklmnopq", false},
		{"uppercase", "Alice", false},
		{"leading-hyphen", "-alice", false},
		{"double-underscore", "ali__ce", false},
		{"single-underscore", "ali_ce", false},
		{"slash", "ali/ce", false},
		{"dotdot", "..", false},
		{"space", "ali ce", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Validate(tt.id); got != tt.ok {
				t.Fatalf("Validate(%q) = %v, want %v", tt.id, got, tt.ok)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		want  string
	}{
		{"snowflake-18-digits", "343535234303787009", "u03787009"},
		{"short-snowflake-5", "12345", "u12345"},
		{"exactly-8", "12345678", "u12345678"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slugify(tt.owner)
			if got != tt.want {
				t.Fatalf("Slugify(%q) = %q, want %q", tt.owner, got, tt.want)
			}
			if got != "" && !Validate(got) {
				t.Fatalf("Slugify(%q) = %q which fails Validate", tt.owner, got)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		owner    string
		want     string
		wantErr  bool
	}{
		{"explicit-wins", "alice", "343535234303787009", "alice", false},
		{"explicit-invalid-errors", "Alice!", "12345678", "", true},
		{"derive-from-owner", "", "343535234303787009", "u03787009", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.explicit, tt.owner)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Resolve(%q,%q) err = %v, wantErr %v", tt.explicit, tt.owner, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Resolve(%q,%q) = %q, want %q", tt.explicit, tt.owner, got, tt.want)
			}
		})
	}
}

func TestResolveWithNoInputIsUnconfigured(t *testing.T) {
	got, err := Resolve("", "")
	if !errors.Is(err, ErrUnconfigured) || got != "" {
		t.Fatalf("Resolve(\"\", \"\") = %q, %v, want ErrUnconfigured", got, err)
	}
}

func TestGenerateGivesAValidFreshID(t *testing.T) {
	first, second := Generate(), Generate()
	if !Validate(first) || !Validate(second) {
		t.Fatalf("Generate gave %q and %q, want valid ids", first, second)
	}
	if first == second {
		t.Fatalf("Generate gave %q twice", first)
	}
}
