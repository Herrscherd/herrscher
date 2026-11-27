// Package reviewloop selects the review threads of a session's pull request that reach its agent,
// and renders them as a block of data.
package reviewloop

import (
	"fmt"
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

// Batch is what one observation hands over: the items to deliver, and the
// position of every thread listed, to be stored once the delivery is claimed.
type Batch struct {
	Items []Item
	Seen  map[string]int
}

// Plan selects, from the threads of a pull request and the number of comments
// already handed over for each, what to deliver next. Resolved threads are
// skipped. Comments from untrusted authors advance the position without being
// delivered.
func Plan(threads []contracts.ReviewThread, seen map[string]int) Batch {
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
		if item, ok := itemOf(thread, fresh[:take]); ok {
			batch.Items = append(batch.Items, item)
		}
	}
	return batch
}

// Trusted reports whether a comment may reach the agent: its author is the
// owner or a member of the repository, and not a bot.
func Trusted(comment contracts.ReviewComment) bool {
	author := comment.Author
	if author.Bot || author.Login == "" {
		return false
	}
	return author.Association == contracts.AssociationOwner || author.Association == contracts.AssociationMember
}

// Render writes the items as one block of data. Every forge field is escaped,
// so no text can close the block or pass for an instruction.
func Render(items []Item) string {
	var out strings.Builder
	out.WriteString(DataOpen + "\n")
	for _, item := range items {
		fmt.Fprintf(&out, "thread: %s\nfile: %s\nline: %d\n", escape(item.Thread), escape(item.Path), item.Line)
		for _, comment := range item.Comments {
			fmt.Fprintf(&out, "author: %s\nbody: %s\n", escape(comment.Author), indent(escape(comment.Body)))
		}
		out.WriteString("\n")
	}
	out.WriteString(DataClose)
	return out.String()
}

func itemOf(thread contracts.ReviewThread, fresh []contracts.ReviewComment) (Item, bool) {
	var comments []Comment
	for _, comment := range fresh {
		if !Trusted(comment) {
			continue
		}
		comments = append(comments, Comment{Author: comment.Author.Login, Body: truncate(comment.Body, MaxBodyBytes)})
	}
	if len(comments) == 0 {
		return Item{}, false
	}
	return Item{Thread: thread.ID, Path: thread.Path, Line: thread.Line, Comments: comments}, true
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
