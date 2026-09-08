package tui

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func panelTab(t *testing.T, events ...contracts.Event) *tab {
	t.Helper()
	m := newModel(&fakeBackend{})
	tb := m.ensureTab("c1")
	for _, e := range events {
		m.renderInto(tb, e)
	}
	return tb
}

func TestTodosReplaceTheWholeList(t *testing.T) {
	tb := panelTab(t,
		contracts.Event{T: "todos", Todos: []contracts.TodoItem{{Text: "a", State: "active"}, {Text: "b", State: "pending"}}},
		contracts.Event{T: "todos", Todos: []contracts.TodoItem{{Text: "a", State: "done"}}},
	)
	if len(tb.todos) != 1 || tb.todos[0].State != "done" {
		t.Fatalf("todos = %+v", tb.todos)
	}
}

func TestASubagentLivesUntilItIsDone(t *testing.T) {
	tb := panelTab(t, contracts.Event{T: "subagent", Subagent: &contracts.Subagent{ID: "t7", Name: "explorer", State: "active"}})
	if len(tb.agents) != 1 || tb.agents[0].since.IsZero() {
		t.Fatalf("agents = %+v", tb.agents)
	}
	tb2 := panelTab(t,
		contracts.Event{T: "subagent", Subagent: &contracts.Subagent{ID: "t7", Name: "explorer", State: "active"}},
		contracts.Event{T: "subagent", Subagent: &contracts.Subagent{ID: "t7", State: "done"}},
	)
	if len(tb2.agents) != 0 {
		t.Fatalf("un agent termine doit quitter le panneau: %+v", tb2.agents)
	}
}

func TestTheEndOfATurnClearsTheAgentsAndAFinishedList(t *testing.T) {
	tb := panelTab(t,
		contracts.Event{T: "subagent", Subagent: &contracts.Subagent{ID: "t7", State: "active"}},
		contracts.Event{T: "todos", Todos: []contracts.TodoItem{{Text: "a", State: "done"}}},
		contracts.Event{T: "reply", Text: "fini", Done: true},
	)
	if len(tb.agents) != 0 || len(tb.todos) != 0 {
		t.Fatalf("agents = %+v todos = %+v", tb.agents, tb.todos)
	}
	still := panelTab(t,
		contracts.Event{T: "todos", Todos: []contracts.TodoItem{{Text: "a", State: "pending"}}},
		contracts.Event{T: "reply", Text: "fini", Done: true},
	)
	if len(still.todos) != 1 {
		t.Fatalf("une liste inachevee doit survivre au tour: %+v", still.todos)
	}
}
