package agent

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const testSecret = "sekret-token-value"

func directoryWith(t *testing.T, environ []string) (*Store, *Directory, *bytes.Buffer, *time.Time) {
	t.Helper()
	store := NewStore(t.TempDir())
	logs := &bytes.Buffer{}
	clock := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	dir := NewDirectory(store, environ, slog.New(slog.NewTextHandler(logs, nil)))
	dir.now = func() time.Time { return clock }
	return store, dir, logs, &clock
}

func TestAgentVoicesSortedWithOwnTokenAndChannelsOfTheGateway(t *testing.T) {
	store, dir, _, _ := directoryWith(t, []string{"CHAT_BOT_TOKEN_RIN=" + testSecret})
	for _, name := range []string{"rin", "anna"} {
		if _, err := store.Create(CreateSpec{Name: name}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	if err := store.SetReply("rin", contracts.ReplyInChannels); err != nil {
		t.Fatalf("set reply: %v", err)
	}
	if err := store.SetChannels("rin", []Channel{{"chat", "111"}, {"other", "-9"}}); err != nil {
		t.Fatalf("set channels: %v", err)
	}
	voices := dir.AgentVoices("chat")
	if len(voices) != 2 || voices[0].Agent != "anna" || voices[1].Agent != "rin" {
		t.Fatalf("voices = %+v; want anna then rin", voices)
	}
	if voices[0].Token.Valid() {
		t.Fatal("anna has no token in the environment and must speak as the gateway's bot")
	}
	if voices[1].Token.Secret() != testSecret || voices[1].Reply != contracts.ReplyInChannels {
		t.Fatalf("rin = %+v; want its token and the channels rule", voices[1])
	}
	if got := voices[1].Channels; len(got) != 1 || got[0] != "111" {
		t.Fatalf("channels on chat = %v; want [111]", got)
	}
	if !dir.HasToken("chat", "rin") || dir.HasToken("other", "rin") {
		t.Fatal("HasToken must follow the gateway kind of the key")
	}
}

func TestAgentVoicesRereadsRulesAfterItsTTL(t *testing.T) {
	store, dir, _, clock := directoryWith(t, nil)
	if _, err := store.Create(CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := dir.AgentVoices("chat")[0].Reply; got != contracts.ReplyOnMention {
		t.Fatalf("reply = %v; want mention", got)
	}
	if err := store.SetReply("rin", contracts.ReplyNever); err != nil {
		t.Fatalf("set reply: %v", err)
	}
	*clock = clock.Add(voiceCacheTTL - time.Second)
	if got := dir.AgentVoices("chat")[0].Reply; got != contracts.ReplyOnMention {
		t.Fatalf("reply within the cache = %v; want the cached mention", got)
	}
	*clock = clock.Add(2 * time.Second)
	if got := dir.AgentVoices("chat")[0].Reply; got != contracts.ReplyNever {
		t.Fatalf("reply after the TTL = %v; want never", got)
	}
}

func TestSharedTokenKeyGivesNoTokenAndLogsOnce(t *testing.T) {
	store, dir, logs, _ := directoryWith(t, []string{"CHAT_BOT_TOKEN_MY_AGENT=" + testSecret})
	for _, name := range []string{"my-agent", "my_agent"} {
		if _, err := store.Create(CreateSpec{Name: name}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	for range 2 {
		for _, voice := range dir.AgentVoices("chat") {
			if voice.Token.Valid() {
				t.Fatalf("%s got a token although its key is shared", voice.Agent)
			}
		}
	}
	if n := strings.Count(logs.String(), "agents share one bot token key"); n != 1 {
		t.Fatalf("clash logged %d times; want once", n)
	}
	if strings.Contains(logs.String(), testSecret) {
		t.Fatal("the token value leaked into the log")
	}
}

func TestUnreadableReplyRuleIsTreatedAsNever(t *testing.T) {
	store, dir, logs, _ := directoryWith(t, nil)
	a, err := store.Create(CreateSpec{Name: "rin"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Home, replyFile), []byte("always\n"), fileMode); err != nil {
		t.Fatalf("write reply: %v", err)
	}
	if got := dir.AgentVoices("chat")[0].Reply; got != contracts.ReplyNever {
		t.Fatalf("reply = %v; want never for an unknown rule", got)
	}
	if !strings.Contains(logs.String(), "treated as never") {
		t.Fatalf("the bad rule was not logged: %q", logs.String())
	}
}

func TestAgentVoicesConcurrentWithRuleWrites(t *testing.T) {
	store, dir, logs, _ := directoryWith(t, []string{"CHAT_BOT_TOKEN_RIN=" + testSecret})
	for _, name := range []string{"rin", "anna"} {
		if _, err := store.Create(CreateSpec{Name: name}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	dir.ttl = 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeRulesAndChannels(t, store, 60)
	}()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			readVoices(t, dir, 60)
		}()
	}
	wg.Wait()
	if strings.Contains(logs.String(), "treated as never") {
		t.Fatalf("a read saw a half-written rule file: %q", logs.String())
	}
}

func writeRulesAndChannels(t *testing.T, store *Store, rounds int) {
	t.Helper()
	rules := []contracts.ReplyRule{contracts.ReplyOnMention, contracts.ReplyInChannels, contracts.ReplyNever}
	for i := range rounds {
		if err := store.SetReply("rin", rules[i%len(rules)]); err != nil {
			t.Errorf("set reply: %v", err)
		}
		if err := store.SetChannels("rin", []Channel{{"chat", strconv.Itoa(i)}}); err != nil {
			t.Errorf("set channels: %v", err)
		}
	}
}

func readVoices(t *testing.T, dir *Directory, rounds int) {
	t.Helper()
	for range rounds {
		if voices := dir.AgentVoices("chat"); len(voices) != 2 {
			t.Errorf("voices = %d; want 2", len(voices))
		}
	}
}

func TestInvalidateVoicesRereadsBeforeTheTTL(t *testing.T) {
	store, dir, _, _ := directoryWith(t, nil)
	if _, err := store.Create(CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := dir.AgentVoices("chat")[0].Reply; got != contracts.ReplyOnMention {
		t.Fatalf("reply = %v; want mention", got)
	}
	if err := store.SetReply("rin", contracts.ReplyNever); err != nil {
		t.Fatalf("set reply: %v", err)
	}
	dir.InvalidateVoices()
	if got := dir.AgentVoices("chat")[0].Reply; got != contracts.ReplyNever {
		t.Fatalf("reply after invalidation = %v; want never", got)
	}
}

func TestRefusedBotTokenIsLoggedBySentinelNeverByValue(t *testing.T) {
	store, dir, logs, _ := directoryWith(t, []string{"CHAT_BOT_TOKEN_RIN=" + testSecret + " extra"})
	if _, err := store.Create(CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if voice := dir.AgentVoices("chat")[0]; voice.Token.Valid() {
		t.Fatal("a token with a space must not be given")
	}
	if !strings.Contains(logs.String(), "invalid bot token") {
		t.Fatalf("the refusal was not logged by its sentinel: %q", logs.String())
	}
	if strings.Contains(logs.String(), testSecret) {
		t.Fatal("the token value leaked into the log")
	}
}
