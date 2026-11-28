package state

import (
	"fmt"
	"maps"
	"slices"

	"github.com/Herrscherd/herrscher/core/internal/reviewloop"
)

// ReviewPosition is how far the review threads of a session's pull request were handed to its
// agent: the number of that request, and for each thread the count of its comments already
// handed over.
type ReviewPosition struct {
	Number int
	Seen   map[string]int
	Own    []reviewloop.Own
}

// ReviewPosition returns the review position recorded on the session.
func (s Session) ReviewPosition() ReviewPosition {
	return ReviewPosition{Number: s.ReviewNumber, Seen: s.ReviewSeen, Own: s.ReviewOwn}
}

// SeenOf returns the counts recorded for pull request number, and nil when the position is of
// another request.
func (p ReviewPosition) SeenOf(number int) map[string]int {
	if p.Number != number {
		return nil
	}
	return p.Seen
}

// Equal reports whether two positions name the same request, the same counts and the same
// replies of the session.
func (p ReviewPosition) Equal(q ReviewPosition) bool {
	return p.Number == q.Number && maps.Equal(p.Seen, q.Seen) && slices.Equal(p.Own, q.Own)
}

// CompareAndSetReviewPosition replaces the review position of the named session by next only when
// it still equals prev, in one step under the state lock. It restores prev and returns the error
// when the state cannot be saved, and reports false when the record no longer equals prev.
func (s *State) CompareAndSetReviewPosition(name string, prev, next ReviewPosition) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.sessionIndexLocked(name)
	if !ok {
		return false, fmt.Errorf("%w: %q", errNoSession, name)
	}
	if !s.Sessions[idx].ReviewPosition().Equal(prev) {
		return false, nil
	}
	setSessionReviewPosition(&s.Sessions[idx], next)
	if err := s.saveLocked(); err != nil {
		setSessionReviewPosition(&s.Sessions[idx], prev)
		return false, err
	}
	return true, nil
}

func setSessionReviewPosition(session *Session, position ReviewPosition) {
	session.ReviewNumber = position.Number
	session.ReviewSeen = position.Seen
	session.ReviewOwn = position.Own
}
