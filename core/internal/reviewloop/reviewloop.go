// Package reviewloop selects the review threads of a session's pull request that reach its agent,
// and renders them as a block of data.
package reviewloop

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
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
	// MaxOwnReplies bounds the replies a session keeps recognising as its own.
	MaxOwnReplies = 20
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

// Own identifies a comment that a session posted itself: its thread, its index in
// that thread, and the digest of its body. The forge gives no identifier for a
// comment, so these three values stand for it.
type Own struct {
	Thread string `json:"thread"`
	Index  int    `json:"index"`
	Digest string `json:"digest"`
}

// Batch is what one observation hands over: the items to deliver, and the
// position of every thread listed, to be stored once the delivery is claimed.
type Batch struct {
	Items []Item
	Seen  map[string]int
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

// Plan selects, from the threads of a pull request and the number of comments
// already handed over for each, what to deliver next. Resolved threads are
// skipped. Comments the trust refuses advance the position without being
// delivered.
func Plan(threads []contracts.ReviewThread, seen map[string]int, own []Own, trust Trust) Batch {
	batch := Batch{Seen: make(map[string]int, len(threads))}
	for _, thread := range threads {
		before := min(seen[thread.ID], len(thread.Comments))
		if thread.Resolved || len(batch.Items) == MaxThreads {
			batch.Seen[thread.ID] = before
			continue
		}
		fresh := thread.Comments[before:]
		take := min(len(fresh), MaxCommentsPerThread)
		batch.Seen[thread.ID] = before + take
		if item, ok := itemOf(thread, before, fresh[:take], own, trust); ok {
			batch.Items = append(batch.Items, item)
		}
	}
	return batch
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

func itemOf(thread contracts.ReviewThread, start int, fresh []contracts.ReviewComment, own []Own, trust Trust) (Item, bool) {
	var comments []Comment
	for offset, comment := range fresh {
		if !trust.Trusts(comment) || isOwn(own, thread.ID, start+offset, comment.Body) {
			continue
		}
		comments = append(comments, Comment{Author: comment.Author.Login, Body: truncate(comment.Body, MaxBodyBytes)})
	}
	if len(comments) == 0 {
		return Item{}, false
	}
	return Item{Thread: thread.ID, Path: thread.Path, Line: thread.Line, Comments: comments}, true
}

// Digest returns the digest that identifies the body of a comment.
func Digest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Remember returns own with the comment of threadID whose body is body added, when
// the threads hold it. The last MaxOwnReplies entries are kept.
func Remember(own []Own, threads []contracts.ReviewThread, threadID, body string) []Own {
	digest := Digest(body)
	for _, thread := range threads {
		if thread.ID != threadID {
			continue
		}
		for index := len(thread.Comments) - 1; index >= 0; index-- {
			if Digest(thread.Comments[index].Body) != digest {
				continue
			}
			entry := Own{Thread: threadID, Index: index, Digest: digest}
			if slices.Contains(own, entry) {
				return own
			}
			next := append(slices.Clone(own), entry)
			return next[max(0, len(next)-MaxOwnReplies):]
		}
	}
	return own
}

func isOwn(own []Own, threadID string, index int, body string) bool {
	return slices.Contains(own, Own{Thread: threadID, Index: index, Digest: Digest(body)})
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
	if symbol == '\n' || symbol == '\r' || symbol == '\t' || symbol == '\u2028' || symbol == '\u2029' {
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
