package host

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

var errApproveAskUnauthenticated = errors.New("approve ask refused: the caller does not hold this session's approval token")

type tokenContextKey struct{}

// TokenVar is the environment variable a session's approval hook reads its token from.
const TokenVar = control.ApprovalKeyVar

func withCallerToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenContextKey{}, token)
}

func callerTokenFrom(ctx context.Context) string {
	token, ok := ctx.Value(tokenContextKey{}).(string)
	if !ok {
		return ""
	}
	return token
}

func (t *sessionTable) issueToken(session string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tokens == nil {
		t.tokens = make(map[string]string)
	}
	token, ok := t.tokens[session]
	if !ok {
		token = rand.Text()
		t.tokens[session] = token
	}
	return token
}

type seedLease struct {
	session string
	done    <-chan struct{}
}

func (l seedLease) alive() bool {
	select {
	case <-l.done:
		return false
	default:
		return true
	}
}

func (t *sessionTable) seedToken(ctx context.Context, session string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seedLeases == nil {
		t.seedLeases = make(map[string]seedLease)
	}
	for token, lease := range t.seedLeases {
		if !lease.alive() {
			delete(t.seedLeases, token)
		}
	}
	token := rand.Text()
	t.seedLeases[token] = seedLease{session: session, done: ctx.Done()}
	return token
}

func (t *sessionTable) revokeTokensExcept(keep func(session string) bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for session := range t.tokens {
		if !keep(session) {
			delete(t.tokens, session)
		}
	}
}

func (t *sessionTable) tokenMatches(session, presented string) bool {
	if presented == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if want, ok := t.tokens[session]; ok && sameToken(want, presented) {
		return true
	}
	for token, lease := range t.seedLeases {
		if lease.session == session && lease.alive() && sameToken(token, presented) {
			return true
		}
	}
	return false
}

func sameToken(want, presented string) bool {
	return subtle.ConstantTimeCompare([]byte(want), []byte(presented)) == 1
}
