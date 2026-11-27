package agent

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestReplyRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.SetReply("rin", contracts.ReplyInChannels); err != nil {
		t.Fatalf("set reply: %v", err)
	}
	got, err := s.Reply("rin")
	if err != nil || got != contracts.ReplyInChannels {
		t.Fatalf("reply = %v, %v; want channels", got, err)
	}
}

func TestMissingReplyFileMeansMention(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Reply("rin")
	if err != nil || got != contracts.ReplyOnMention {
		t.Fatalf("reply = %v, %v; want mention", got, err)
	}
}

func TestUnknownReplyFileIsAnError(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "rin"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Home, replyFile), []byte("sometimes\n"), fileMode); err != nil {
		t.Fatalf("write reply: %v", err)
	}
	got, err := s.Reply("rin")
	if !errors.Is(err, contracts.ErrUnknownReplyRule) {
		t.Fatalf("err = %v; want unknown reply rule", err)
	}
	if got != contracts.ReplyNever {
		t.Fatalf("reply = %v on error; want never", got)
	}
}

func TestSetReplyRefusesUnknownRuleAndWritesNothing(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "rin"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.SetReply("rin", contracts.ReplyRule(9)); !errors.Is(err, contracts.ErrUnknownReplyRule) {
		t.Fatalf("err = %v; want unknown reply rule", err)
	}
	if _, err := os.Stat(filepath.Join(a.Home, replyFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reply file written for an unknown rule: %v", err)
	}
}

func TestParseChannel(t *testing.T) {
	for _, tc := range []struct {
		text string
		ok   bool
	}{
		{"discord:123456789012345678", true},
		{"telegram:-1001234567890", true},
		{"discord", false},
		{":123", false},
		{"discord:", false},
		{"discord:12 34", false},
	} {
		_, err := ParseChannel(tc.text)
		if (err == nil) != tc.ok {
			t.Errorf("ParseChannel(%q) err = %v; want ok=%v", tc.text, err, tc.ok)
		}
		if err != nil && !errors.Is(err, ErrBadChannel) {
			t.Errorf("ParseChannel(%q) err = %v; want ErrBadChannel", tc.text, err)
		}
	}
}

func TestChannelsSortedDedupedAndBadLineRefused(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	written := []Channel{{"telegram", "-1"}, {"discord", "2"}, {"discord", "2"}}
	if err := s.SetChannels("rin", written); err != nil {
		t.Fatalf("set channels: %v", err)
	}
	got, err := s.Channels("rin")
	if err != nil {
		t.Fatalf("channels: %v", err)
	}
	want := []Channel{{"discord", "2"}, {"telegram", "-1"}}
	if !slices.Equal(got, want) {
		t.Fatalf("channels = %v; want %v", got, want)
	}
	if err := s.SetChannels("rin", []Channel{{"discord", ""}}); !errors.Is(err, ErrBadChannel) {
		t.Fatalf("err = %v; want ErrBadChannel", err)
	}
	kept, err := s.Channels("rin")
	if err != nil || !slices.Equal(kept, want) {
		t.Fatalf("refused write changed the channels: %v, %v", kept, err)
	}
}

func TestBotTokenKey(t *testing.T) {
	for _, tc := range []struct{ kind, name, want string }{
		{"discord", "rin", "DISCORD_BOT_TOKEN_RIN"},
		{"telegram", "my-agent", "TELEGRAM_BOT_TOKEN_MY_AGENT"},
	} {
		if got := BotTokenKey(tc.kind, tc.name); got != tc.want {
			t.Errorf("BotTokenKey(%q, %q) = %q; want %q", tc.kind, tc.name, got, tc.want)
		}
	}
	if BotTokenKey("discord", "my-agent") != BotTokenKey("discord", "my_agent") {
		t.Fatal("names differing by a dash and an underscore must share a key, so the clash is detectable")
	}
}
