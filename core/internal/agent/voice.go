package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
)

const (
	replyFile    = "reply"
	channelsFile = "channels"
	botInfix     = "_BOT_TOKEN_"
)

// ErrBadChannel is returned for a channel that is not gateway:conversation, with
// both sides non-empty and no blank inside it.
var ErrBadChannel = errors.New("channel is not gateway:conversation")

// Channel is one conversation of one gateway where an agent may answer.
type Channel struct {
	Gateway      string
	Conversation string
}

// ParseChannel reads a channel from its text form, gateway:conversation.
func ParseChannel(text string) (Channel, error) {
	gateway, conversation, found := strings.Cut(strings.TrimSpace(text), ":")
	channel := Channel{Gateway: gateway, Conversation: conversation}
	if !found || !channel.valid() {
		return Channel{}, ErrBadChannel
	}
	return channel, nil
}

func (c Channel) valid() bool {
	return c.Gateway != "" && c.Conversation != "" && !strings.ContainsAny(c.Gateway, ": \t") && !strings.ContainsAny(c.Conversation, " \t")
}

// String writes the text form of the channel, gateway:conversation.
func (c Channel) String() string { return c.Gateway + ":" + c.Conversation }

// BotTokenKey names the daemon secret that holds the bot token of one agent on
// one gateway kind: the kind and the agent name, upper-cased, with dashes turned
// into underscores, as in CHAT_BOT_TOKEN_RIN.
func BotTokenKey(kind, agentName string) string {
	return envSegment(kind) + botInfix + envSegment(agentName)
}

func envSegment(text string) string {
	return strings.ToUpper(strings.ReplaceAll(text, "-", "_"))
}

// Reply reads the reply rule of an agent. A missing reply file means
// [contracts.ReplyOnMention]; a file holding no rule is an error.
func (s *Store) Reply(name string) (contracts.ReplyRule, error) {
	home, err := s.home(name)
	if err != nil {
		return contracts.ReplyNever, err
	}
	buf, err := os.ReadFile(filepath.Clean(filepath.Join(home, replyFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return contracts.ReplyOnMention, nil
	}
	if err != nil {
		return contracts.ReplyNever, fmt.Errorf("read reply of %q: %w", name, err)
	}
	rule, err := contracts.ParseReplyRule(strings.TrimSpace(string(buf)))
	if err != nil {
		return contracts.ReplyNever, fmt.Errorf("reply of %q: %w", name, err)
	}
	return rule, nil
}

// SetReply writes the reply rule of an existing agent. A rule that names no
// known rule is refused with [contracts.ErrUnknownReplyRule].
func (s *Store) SetReply(name string, rule contracts.ReplyRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	home, err := s.home(name)
	if err != nil {
		return err
	}
	text, err := ruleFileText(rule)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(home, replyFile), text, fileMode)
}

// Channels reads the channels of an agent, in the order of the file. A missing
// channels file means none; a line that is not a channel is an error.
func (s *Store) Channels(name string) ([]Channel, error) {
	home, err := s.home(name)
	if err != nil {
		return nil, err
	}
	buf, err := os.ReadFile(filepath.Clean(filepath.Join(home, channelsFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read channels of %q: %w", name, err)
	}
	var channels []Channel
	for _, line := range strings.Split(string(buf), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		channel, err := ParseChannel(line)
		if err != nil {
			return nil, fmt.Errorf("channels of %q: %w", name, err)
		}
		channels = append(channels, channel)
	}
	return channels, nil
}

// SetReplyWithChannels replaces the channels and the reply rule of an existing
// agent. Both are checked before anything is written, then the channels are
// written and the rule after them. When the rule cannot be written, the channels
// are put back as they were, or removed when the agent had none, and the rule
// error is returned.
func (s *Store) SetReplyWithChannels(name string, rule contracts.ReplyRule, channels []Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	home, err := s.home(name)
	if err != nil {
		return err
	}
	ruleText, err := ruleFileText(rule)
	if err != nil {
		return err
	}
	channelsText, err := channelsFileText(channels)
	if err != nil {
		return err
	}
	channelsPath := filepath.Join(home, channelsFile)
	previous, existed, err := readOptional(channelsPath)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(channelsPath, channelsText, fileMode); err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(home, replyFile), ruleText, fileMode); err != nil {
		return errors.Join(err, leftAsWritten(name, restoreChannels(channelsPath, previous, existed)))
	}
	return nil
}

func ruleFileText(rule contracts.ReplyRule) ([]byte, error) {
	text, err := rule.MarshalText()
	if err != nil {
		return nil, err
	}
	return append(text, '\n'), nil
}

func channelsFileText(channels []Channel) ([]byte, error) {
	var out strings.Builder
	for _, channel := range normalizeChannels(channels) {
		if !channel.valid() {
			return nil, ErrBadChannel
		}
		out.WriteString(channel.String())
		out.WriteByte('\n')
	}
	return []byte(out.String()), nil
}

func readOptional(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

func leftAsWritten(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore channels of %q, left as written: %w", name, err)
}

func restoreChannels(path string, previous []byte, existed bool) error {
	if existed {
		return atomicfile.Write(path, previous, fileMode)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func normalizeChannels(channels []Channel) []Channel {
	sorted := slices.Clone(channels)
	slices.SortFunc(sorted, func(a, b Channel) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(sorted)
}

func (s *Store) home(name string) (string, error) {
	if !validateName(name) {
		return "", &InvalidNameError{Name: name}
	}
	home := filepath.Join(s.root, name)
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return "", &NotFoundError{Name: name}
	}
	return home, nil
}
