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

func (t *sessionTable) tokenMatches(session, presented string) bool {
	t.mu.Lock()
	want, ok := t.tokens[session]
	t.mu.Unlock()
	return ok && presented != "" && subtle.ConstantTimeCompare([]byte(want), []byte(presented)) == 1
}
