package host

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestSaveAndCatchUpDoNotWaitForTheForge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newCIHarness(t)
		h.failingWithJob()
		h.forge.block = make(chan struct{})
		var unblock sync.Once
		w := newSessionPRWatcher(clitext.NewMessages(), fixedLanguage(i18n.English), h.st, nil, nil, h.seeds.seed, h.notify)
		group := &prGroup{key: prGroupKey{repo: prRepoRef(t).String(), number: prTestNumber}, forge: h.forge, members: map[string]bool{"alpha": true}}
		ctx := t.Context()
		defer w.watches.Wait()
		defer unblock.Do(func() { close(h.forge.block) })

		saved := make(chan error, 1)
		go func() { saved <- w.save(group)(ctx, h.pull("1", contracts.ChecksFailed)) }()
		select {
		case err := <-saved:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("save waited for the forge")
		}

		caught := make(chan struct{})
		go func() {
			w.catchUp(ctx, group, "alpha")
			close(caught)
		}()
		select {
		case <-caught:
		case <-time.After(time.Second):
			t.Fatal("catch-up waited for the forge")
		}
	})
}

func TestOneObservationRunsPerPullRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newCIHarness(t)
		h.failingWithJob()
		h.forge.block = make(chan struct{})
		var unblock sync.Once
		w := newSessionPRWatcher(clitext.NewMessages(), fixedLanguage(i18n.English), h.st, nil, nil, h.seeds.seed, h.notify)
		group := &prGroup{key: prGroupKey{repo: prRepoRef(t).String(), number: prTestNumber}, forge: h.forge, members: map[string]bool{"alpha": true}}
		ctx := t.Context()
		defer w.watches.Wait()
		defer unblock.Do(func() { close(h.forge.block) })
		save := w.save(group)

		if err := save(ctx, h.pull("1", contracts.ChecksFailed)); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !group.saveMu.TryLock() {
			t.Fatal("the save lock is held while the forge is observed")
		}
		group.saveMu.Unlock()
		if err := save(ctx, h.pull("2", contracts.ChecksFailed)); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if h.forge.checkCalls != 1 {
			t.Fatalf("observations started = %d while one is in flight, want 1", h.forge.checkCalls)
		}

		unblock.Do(func() { close(h.forge.block) })
		synctest.Wait()
		if len(h.seeds.tasks) != 1 {
			t.Fatalf("messages = %d, want only the newest head nudged", len(h.seeds.tasks))
		}
		if got := h.nudge(); got.Head != commitOf(t, "2").String() || got.Attempts != 1 {
			t.Fatalf("record = %+v, want attempt 1 on head 2", got)
		}
	})
}
