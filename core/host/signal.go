package host

import "sync"

type changeNotifier struct {
	mu sync.Mutex
	ch chan struct{}
}

func (n *changeNotifier) watch() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ch == nil {
		n.ch = make(chan struct{})
	}
	return n.ch
}

func (n *changeNotifier) signal() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ch != nil {
		close(n.ch)
		n.ch = nil
	}
}
