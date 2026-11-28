// Package reviewloop selects the review threads of a session's pull request that reach its agent,
// and renders them as a block of data.
package reviewloop

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	// MaxThreads bounds the threads one message hands to the agent.
	MaxThreads = 8
	// MaxCommentsPerThread bounds the new comments of one thread in one message.
	MaxCommentsPerThread = 5
	// MaxBodyBytes bounds the body of one comment, cut at a rune boundary.
	MaxBodyBytes = 1000
	// MaxOwnReplies bounds the comments a session keeps recognising as its own.
	MaxOwnReplies = 20
	// MaxPendingReplies bounds the replies of a session waiting to be seen on the forge.
	MaxPendingReplies = 20
	// PendingTTL is how long a reply waits to be seen on the forge before it is forgotten.
	PendingTTL = 24 * time.Hour
	// ClockMargin is the tolerance between the clock of the host and the clock of the forge.
	ClockMargin = 2 * time.Minute
	// DataOpen opens the block that holds forge text.
	DataOpen = "<donnees-revue>"
	// DataClose closes the block that holds forge text.
	DataClose      = "</donnees-revue>"
	truncationMark = "..."
	bodyIndent     = "\n  "
)

// Item is one thread that carries new trusted comments.
type Item struct {
	Thread   string
	Path     string
	Line     int
	Comments []Comment
}

// Comment is one trusted message of a thread, as the agent is shown it.
type Comment struct {
	Author string
	Body   string
}

// Mark is how far a thread was handed over: every comment created before At was seen,
// and the comments created at At are seen when their digest is in Digests.
type Mark struct {
	At      time.Time `json:"at"`
	Digests []string  `json:"digests,omitempty"`
}

// Equal reports whether two marks name the same instant and the same digests.
func (m Mark) Equal(n Mark) bool {
	return m.At.Equal(n.At) && slices.Equal(m.Digests, n.Digests)
}

// Own identifies a comment that a session posted itself, by its thread, the digest of its
// body and its creation time. The forge gives no identifier for a comment.
type Own struct {
	Thread    string    `json:"thread"`
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"createdAt"`
}

// Equal reports whether two records name the same comment.
func (o Own) Equal(p Own) bool {
	return o.Thread == p.Thread && o.Digest == p.Digest && o.CreatedAt.Equal(p.CreatedAt)
}

// Pending is a reply the session published and that the forge may not show yet. Login is the
// account that posted it, empty when unknown.
type Pending struct {
	Thread   string    `json:"thread"`
	Digest   string    `json:"digest"`
	Login    string    `json:"login,omitempty"`
	PostedAt time.Time `json:"postedAt"`
}

// Equal reports whether two pending replies are the same.
func (p Pending) Equal(q Pending) bool {
	return p.Thread == q.Thread && p.Digest == q.Digest && p.Login == q.Login && p.PostedAt.Equal(q.PostedAt)
}

// Batch is what one observation hands over: the items to deliver, and the mark of every
// thread listed, to be stored once the delivery is claimed.
type Batch struct {
	Items []Item
	Marks map[string]Mark
}

// Trust decides which comment authors reach the agent. The zero value trusts the
// owner and the members of the repository only.
type Trust struct {
	logins map[string]bool
}

// NewTrust returns a Trust that also accepts the listed logins, whatever their
// association with the repository. Logins are compared without regard to case.
func NewTrust(logins map[string]bool) Trust {
	trusted := make(map[string]bool, len(logins))
	for login, listed := range logins {
		if listed {
			trusted[strings.ToLower(login)] = true
		}
	}
	return Trust{logins: trusted}
}

// Trusts reports whether a comment may reach the agent. A bot never does. Any
// other author does when it is the owner or a member of the repository, or when
// its login is listed.
func (t Trust) Trusts(comment contracts.ReviewComment) bool {
	author := comment.Author
	if author.Bot || author.Login == "" {
		return false
	}
	if author.Association == contracts.AssociationOwner || author.Association == contracts.AssociationMember {
		return true
	}
	return t.logins[strings.ToLower(author.Login)]
}

// Plan selects, from the threads of a pull request, the marks of the threads already handed
// over and the comments of the session, what to deliver next. Resolved threads are skipped.
// Comments the trust refuses, and the comments the session posted, advance the mark without
// being delivered.
func Plan(threads []contracts.ReviewThread, marks map[string]Mark, own []Own, trust Trust) Batch {
	batch := Batch{Marks: make(map[string]Mark, len(threads))}
	for _, thread := range threads {
		mark, marked := marks[thread.ID]
		if marked {
			batch.Marks[thread.ID] = mark
		}
		if thread.Resolved || len(batch.Items) == MaxThreads {
			continue
		}
		fresh := unseen(thread.Comments, mark, marked)
		take := min(len(fresh), MaxCommentsPerThread)
		if take == 0 {
			continue
		}
		batch.Marks[thread.ID] = advance(mark, marked, fresh[:take])
		if item, ok := itemOf(thread, fresh[:take], own, trust); ok {
			batch.Items = append(batch.Items, item)
		}
	}
	return batch
}

// Initialise returns the marks of the threads of a state written with counts per thread: each
// thread known to the counts is marked at its latest comment, so nothing it already held is
// handed over again.
func Initialise(threads []contracts.ReviewThread, counted map[string]int) map[string]Mark {
	marks := make(map[string]Mark, len(counted))
	for _, thread := range threads {
		if _, known := counted[thread.ID]; !known || len(thread.Comments) == 0 {
			continue
		}
		marks[thread.ID] = advance(Mark{}, false, thread.Comments)
	}
	return marks
}

// Settle matches the pending replies with the comments the threads now hold. A pending reply
// that matches becomes an own comment and leaves the pending list. A pending reply that no
// comment matches stays until its age reaches PendingTTL. Both lists are bounded.
func Settle(threads []contracts.ReviewThread, own []Own, pending []Pending, now time.Time) ([]Own, []Pending) {
	var kept []Pending
	for _, entry := range pending {
		if now.Sub(entry.PostedAt) > PendingTTL {
			continue
		}
		if match, ok := matchOf(threads, own, entry); ok {
			own = append(slices.Clone(own), match)
			continue
		}
		kept = append(kept, entry)
	}
	return bound(own, MaxOwnReplies), bound(kept, MaxPendingReplies)
}

// Expect returns pending with entry added, without the replies older than PendingTTL at now.
func Expect(pending []Pending, entry Pending, now time.Time) []Pending {
	var kept []Pending
	for _, old := range pending {
		if now.Sub(old.PostedAt) <= PendingTTL {
			kept = append(kept, old)
		}
	}
	return bound(append(kept, entry), MaxPendingReplies)
}

// IsOwn reports whether a comment of threadID is one the session posted.
func IsOwn(own []Own, threadID string, comment contracts.ReviewComment) bool {
	record := Own{Thread: threadID, Digest: Digest(comment.Body), CreatedAt: comment.CreatedAt}
	return slices.ContainsFunc(own, record.Equal)
}

// Digest returns the digest that identifies the body of a comment.
func Digest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Render writes the items as one block of data. Every forge field is escaped,
// so no text can close the block or pass for an instruction. The fields kept on
// one line are flattened first, so no text can start a line of its own.
func Render(items []Item) string {
	var out strings.Builder
	out.WriteString(DataOpen + "\n")
	for _, item := range items {
		fmt.Fprintf(&out, "thread: %s\nfile: %s\nline: %d\n", inline(item.Thread), inline(item.Path), item.Line)
		for _, comment := range item.Comments {
			fmt.Fprintf(&out, "author: %s\nbody: %s\n", inline(comment.Author), indent(escape(comment.Body)))
		}
		out.WriteString("\n")
	}
	out.WriteString(DataClose)
	return out.String()
}

func unseen(comments []contracts.ReviewComment, mark Mark, marked bool) []contracts.ReviewComment {
	var fresh []contracts.ReviewComment
	for _, comment := range comments {
		if marked && isSeen(comment, mark) {
			continue
		}
		fresh = append(fresh, comment)
	}
	return fresh
}

func isSeen(comment contracts.ReviewComment, mark Mark) bool {
	if comment.CreatedAt.Before(mark.At) {
		return true
	}
	return comment.CreatedAt.Equal(mark.At) && slices.Contains(mark.Digests, Digest(comment.Body))
}

func advance(mark Mark, marked bool, handed []contracts.ReviewComment) Mark {
	last := handed[len(handed)-1].CreatedAt
	next := Mark{At: last}
	if marked && mark.At.Equal(last) {
		next.Digests = slices.Clone(mark.Digests)
	}
	for _, comment := range handed {
		digest := Digest(comment.Body)
		if comment.CreatedAt.Equal(last) && !slices.Contains(next.Digests, digest) {
			next.Digests = append(next.Digests, digest)
		}
	}
	return next
}

func matchOf(threads []contracts.ReviewThread, own []Own, entry Pending) (Own, bool) {
	earliest := entry.PostedAt.Add(-ClockMargin)
	for _, thread := range threads {
		if thread.ID != entry.Thread {
			continue
		}
		for _, comment := range thread.Comments {
			if Digest(comment.Body) != entry.Digest || comment.CreatedAt.Before(earliest) {
				continue
			}
			if entry.Login != "" && !strings.EqualFold(comment.Author.Login, entry.Login) {
				continue
			}
			match := Own{Thread: thread.ID, Digest: entry.Digest, CreatedAt: comment.CreatedAt}
			if !slices.ContainsFunc(own, match.Equal) {
				return match, true
			}
		}
	}
	return Own{}, false
}

func itemOf(thread contracts.ReviewThread, fresh []contracts.ReviewComment, own []Own, trust Trust) (Item, bool) {
	var comments []Comment
	for _, comment := range fresh {
		if !trust.Trusts(comment) || IsOwn(own, thread.ID, comment) {
			continue
		}
		comments = append(comments, Comment{Author: comment.Author.Login, Body: truncate(comment.Body, MaxBodyBytes)})
	}
	if len(comments) == 0 {
		return Item{}, false
	}
	return Item{Thread: thread.ID, Path: thread.Path, Line: thread.Line, Comments: comments}, true
}

func bound[T any](items []T, limit int) []T {
	if len(items) <= limit {
		return items
	}
	return slices.Clone(items[len(items)-limit:])
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + truncationMark
}

func inline(text string) string {
	return escape(strings.Map(flatten, text))
}

func flatten(symbol rune) rune {
	if symbol == '\n' || symbol == '\r' || symbol == '\t' || symbol == ' ' || symbol == ' ' {
		return ' '
	}
	if unicode.Is(unicode.Cf, symbol) {
		return -1
	}
	return symbol
}

func escape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(strings.Map(dropControl, text))
}

func dropControl(symbol rune) rune {
	if symbol == '\n' || symbol == '\t' {
		return symbol
	}
	if unicode.IsControl(symbol) {
		return -1
	}
	return symbol
}

func indent(text string) string {
	return strings.ReplaceAll(text, "\n", bodyIndent)
}
