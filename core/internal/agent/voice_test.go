package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	if err = os.WriteFile(filepath.Join(a.Home, replyFile), []byte("sometimes\n"), fileMode); err != nil {
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
		{"chat:123456789012345678", true},
		{"other:-1001234567890", true},
		{"chat", false},
		{":123", false},
		{"chat:", false},
		{"chat:12 34", false},
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
	written := []Channel{{"other", "-1"}, {"chat", "2"}, {"chat", "2"}}
	if err := s.SetReplyWithChannels("rin", contracts.ReplyInChannels, written); err != nil {
		t.Fatalf("set channels: %v", err)
	}
	got, err := s.Channels("rin")
	if err != nil {
		t.Fatalf("channels: %v", err)
	}
	want := []Channel{{"chat", "2"}, {"other", "-1"}}
	if !slices.Equal(got, want) {
		t.Fatalf("channels = %v; want %v", got, want)
	}
	if err = s.SetReplyWithChannels("rin", contracts.ReplyInChannels, []Channel{{"chat", ""}}); !errors.Is(err, ErrBadChannel) {
		t.Fatalf("err = %v; want ErrBadChannel", err)
	}
	kept, err := s.Channels("rin")
	if err != nil || !slices.Equal(kept, want) {
		t.Fatalf("refused write changed the channels: %v, %v", kept, err)
	}
}

func TestBotTokenKey(t *testing.T) {
	for _, tc := range []struct{ kind, name, want string }{
		{"chat", "rin", "CHAT_BOT_TOKEN_RIN"},
		{"other", "my-agent", "OTHER_BOT_TOKEN_MY_AGENT"},
	} {
		if got := BotTokenKey(tc.kind, tc.name); got != tc.want {
			t.Errorf("BotTokenKey(%q, %q) = %q; want %q", tc.kind, tc.name, got, tc.want)
		}
	}
	if BotTokenKey("chat", "my-agent") != BotTokenKey("chat", "my_agent") {
		t.Fatal("names differing by a dash and an underscore must share a key, so the clash is detectable")
	}
}

func TestChannelsReportsABadLineInTheFile(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "rin"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Home, channelsFile), []byte("chat:111\nnocolon\n"), fileMode); err != nil {
		t.Fatalf("write channels: %v", err)
	}
	if _, err := s.Channels("rin"); !errors.Is(err, ErrBadChannel) {
		t.Fatalf("err = %v; want ErrBadChannel for the bad line", err)
	}
}

func TestSetReplyWithChannelsRemovesChannelsFileWhenRuleFails(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "rin"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.Mkdir(filepath.Join(a.Home, replyFile), 0o755); err != nil {
		t.Fatalf("block the reply file: %v", err)
	}
	if err := s.SetReplyWithChannels("rin", contracts.ReplyInChannels, []Channel{{"chat", "111"}}); err == nil {
		t.Fatal("a rule that cannot be written must fail")
	}
	if _, err := os.Stat(filepath.Join(a.Home, channelsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("channels file after a failed rule: %v; want none", err)
	}
}

func TestConcurrentFailedRuleWritesRestoreTheChannelsTheyFound(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "rin"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	seed := []Channel{{"chat", "1"}}
	if err = s.SetReplyWithChannels("rin", contracts.ReplyInChannels, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err = os.Remove(filepath.Join(a.Home, replyFile)); err != nil {
		t.Fatalf("remove reply: %v", err)
	}
	if err = os.Mkdir(filepath.Join(a.Home, replyFile), 0o755); err != nil {
		t.Fatalf("block the reply file: %v", err)
	}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			writeErr := s.SetReplyWithChannels("rin", contracts.ReplyInChannels, []Channel{{"chat", strconv.Itoa(100 + i)}})
			if writeErr == nil {
				t.Errorf("a rule written onto a directory must fail")
			}
		}()
	}
	wg.Wait()
	got, err := s.Channels("rin")
	if err != nil || !slices.Equal(got, seed) {
		t.Fatalf("channels after failed rule writes = %v, %v; want the seed %v", got, err, seed)
	}
}

func TestDoubleFailureNamesTheAgentAndKeepsBothCauses(t *testing.T) {
	path := filepath.Join(t.TempDir(), channelsFile)
	if err := os.MkdirAll(filepath.Join(path, "occupied"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := restoreChannels(path, nil, false); err == nil {
		t.Fatal("restoreChannels over a non-empty directory = nil; want the remove error")
	}
	errWrite := errors.New("write reply")
	err := channelsLeftBehind("rin", errWrite, path, nil, false)
	for _, want := range []string{`restore channels of "rin"`, "left as written", "write reply"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("double failure = %q; want it to contain %q", err, want)
		}
	}
	if !errors.Is(err, errWrite) {
		t.Fatalf("errors.Is lost the write failure: %v", err)
	}
	removeErr, ok := errors.AsType[*fs.PathError](err)
	if !ok || removeErr.Path != path || removeErr.Op != "remove" {
		t.Fatalf("the remove failure is not in the chain: %v", err)
	}
	if !errors.Is(err, removeErr.Err) {
		t.Fatalf("errors.Is lost the remove cause: %v", err)
	}
}
