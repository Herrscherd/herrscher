package agent

import (
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const voiceCacheTTL = 5 * time.Second

// Directory lists the agents of a Store with how each speaks on a gateway. The
// bot tokens are a snapshot of the environment taken when the Directory is built;
// reply rules and channels are reread from disk when the cache is older than its
// time to live.
type Directory struct {
	store  *Store
	tokens map[string]string
	log    *slog.Logger
	now    func() time.Time
	ttl    time.Duration

	mu       sync.Mutex
	loaded   bool
	loadedAt time.Time
	rules    []voiceRule
	warned   map[string]bool
}

type voiceRule struct {
	agent    string
	reply    contracts.ReplyRule
	channels []Channel
}

// NewDirectory builds the directory of store. environ is the environment of the
// daemon, as os.Environ returns it: only the entries naming a bot token are kept.
// A nil log uses the default logger.
func NewDirectory(store *Store, environ []string, log *slog.Logger) *Directory {
	if log == nil {
		log = slog.Default()
	}
	return &Directory{
		store:  store,
		tokens: tokensOf(environ),
		log:    log,
		now:    time.Now,
		ttl:    voiceCacheTTL,
		warned: make(map[string]bool),
	}
}

func tokensOf(environ []string) map[string]string {
	tokens := make(map[string]string)
	for _, entry := range environ {
		key, value, found := strings.Cut(entry, "=")
		if found && value != "" && strings.Contains(key, botInfix) {
			tokens[key] = value
		}
	}
	return tokens
}

// AgentVoices lists every agent with how it speaks on the gateway of that kind,
// sorted by agent name. An agent whose rules cannot be read is listed as never
// answering. A bot token shared by two agents is given to neither of them.
func (d *Directory) AgentVoices(gateway string) []contracts.AgentVoice {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rules := d.currentLocked()
	voices := make([]contracts.AgentVoice, 0, len(rules))
	for _, rule := range rules {
		voices = append(voices, contracts.AgentVoice{
			Agent:    rule.agent,
			Token:    d.tokenLocked(gateway, rule.agent, rules),
			Reply:    rule.reply,
			Channels: channelsOn(gateway, rule.channels),
		})
	}
	slices.SortFunc(voices, func(a, b contracts.AgentVoice) int { return strings.Compare(a.Agent, b.Agent) })
	return voices
}

// InvalidateVoices makes the next read reload the rules from disk.
func (d *Directory) InvalidateVoices() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loaded = false
}

// HasToken reports whether an agent has its own bot token on the gateway of
// that kind. The token itself is never returned.
func (d *Directory) HasToken(gateway, agent string) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.tokenLocked(gateway, agent, d.currentLocked()).Valid()
}

func (d *Directory) tokenLocked(gateway, agent string, rules []voiceRule) contracts.BotToken {
	key := BotTokenKey(gateway, agent)
	secret, ok := d.tokens[key]
	if !ok {
		return contracts.BotToken{}
	}
	if d.sharedLocked(gateway, key, rules) {
		return contracts.BotToken{}
	}
	token, err := contracts.NewBotToken(secret)
	if err != nil {
		if d.firstTime("token:" + key) {
			d.log.Warn("bot token refused, the agent speaks as the gateway's own bot", "key", key, "reason", refusalOf(err))
		}
		return contracts.BotToken{}
	}
	return token
}

func refusalOf(err error) error {
	for _, sentinel := range []error{contracts.ErrEmptyBotToken, contracts.ErrInvalidBotToken} {
		if errors.Is(err, sentinel) {
			return sentinel
		}
	}
	return err
}

func (d *Directory) sharedLocked(gateway, key string, rules []voiceRule) bool {
	holders := 0
	for _, rule := range rules {
		if BotTokenKey(gateway, rule.agent) == key {
			holders++
		}
	}
	if holders <= 1 {
		return false
	}
	if d.firstTime("clash:" + key) {
		d.log.Warn("agents share one bot token key, none of them gets its token", "key", key)
	}
	return true
}

func (d *Directory) currentLocked() []voiceRule {
	now := d.now()
	if d.loaded && now.Sub(d.loadedAt) < d.ttl {
		return d.rules
	}
	d.rules = d.readLocked()
	d.loaded = true
	d.loadedAt = now
	return d.rules
}

func (d *Directory) readLocked() []voiceRule {
	names, err := d.store.names()
	if err != nil {
		if d.firstTime("list:" + err.Error()) {
			d.log.Warn("agent list unreadable, agents keep their last rules", "err", err)
		}
		return d.rules
	}
	rules := make([]voiceRule, 0, len(names))
	for _, name := range names {
		rules = append(rules, d.ruleOf(name))
	}
	return rules
}

func (d *Directory) ruleOf(name string) voiceRule {
	reply, err := d.store.Reply(name)
	if err != nil {
		if d.firstTime("reply:" + name + err.Error()) {
			d.log.Warn("agent reply rule unreadable, the agent is treated as never", "agent", name, "err", err)
		}
		return voiceRule{agent: name, reply: contracts.ReplyNever}
	}
	channels, err := d.store.Channels(name)
	if err != nil {
		if d.firstTime("channels:" + name + err.Error()) {
			d.log.Warn("agent channels unreadable, the agent is treated as never", "agent", name, "err", err)
		}
		return voiceRule{agent: name, reply: contracts.ReplyNever}
	}
	return voiceRule{agent: name, reply: reply, channels: channels}
}

func (d *Directory) firstTime(key string) bool {
	if d.warned[key] {
		return false
	}
	d.warned[key] = true
	return true
}

func channelsOn(gateway string, channels []Channel) []string {
	out := make([]string, 0, len(channels))
	for _, channel := range channels {
		if channel.Gateway == gateway {
			out = append(out, channel.Conversation)
		}
	}
	return out
}
