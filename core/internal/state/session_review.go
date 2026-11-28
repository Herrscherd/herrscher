package state

import (
	"fmt"
	"maps"
	"slices"

	"github.com/Herrscherd/herrscher/core/internal/reviewloop"
)

// ReviewPosition is how far the review threads of a session's pull request were handed to its
// agent: the number of that request, the mark of each thread, the replies the session posted and
// the replies it still waits to see on the forge. Legacy holds the counts of a state written
// before the marks existed, until the first observation converts them.
type ReviewPosition struct {
	Number  int
	Marks   map[string]reviewloop.Mark
	Own     []reviewloop.Own
	Pending []reviewloop.Pending
	Legacy  map[string]int
}

// ReviewPosition returns the review position recorded on the session.
func (s Session) ReviewPosition() ReviewPosition {
	return ReviewPosition{
		Number:  s.ReviewNumber,
		Marks:   s.ReviewMarks,
		Own:     s.ReviewOwn,
		Pending: s.ReviewPending,
		Legacy:  s.ReviewSeen,
	}
}

// ForRequest returns the position as it applies to pull request number: the marks, replies and
// legacy counts of another request do not apply, so they are left empty.
func (p ReviewPosition) ForRequest(number int) ReviewPosition {
	if p.Number == number {
		return p
	}
	return ReviewPosition{Number: number}
}

// Equal reports whether two positions name the same request and hold the same marks, replies
// and counts.
func (p ReviewPosition) Equal(other ReviewPosition) bool {
	return p.Number == other.Number &&
		maps.EqualFunc(p.Marks, other.Marks, reviewloop.Mark.Equal) &&
		slices.EqualFunc(p.Own, other.Own, reviewloop.Own.Equal) &&
		slices.EqualFunc(p.Pending, other.Pending, reviewloop.Pending.Equal) &&
		maps.Equal(p.Legacy, other.Legacy)
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
	session.ReviewMarks = position.Marks
	session.ReviewOwn = position.Own
	session.ReviewPending = position.Pending
	session.ReviewSeen = position.Legacy
}
