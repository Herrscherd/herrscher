package keyproxy

import (
	"errors"
	"io"
	"net/http"
	"sync"
)

const maxBufferedBytes = 256 << 20

var errBodyBudget = errors.New("key proxy is holding too many request bodies, retry shortly")

type bodyBudget struct {
	mu    sync.Mutex
	used  int64
	limit int64
}

func (b *bodyBudget) take(size int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used+size > b.limit {
		return false
	}
	b.used += size
	return true
}

func (b *bodyBudget) give(size int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used -= size
}

type budgetedReader struct {
	source io.Reader
	budget *bodyBudget
	taken  int64
}

func (r *budgetedReader) Read(chunk []byte) (int, error) {
	read, err := r.source.Read(chunk)
	if read == 0 {
		return 0, err
	}
	if !r.budget.take(int64(read)) {
		return 0, errBodyBudget
	}
	r.taken += int64(read)
	return read, err
}

func (p *Proxy) bufferBody(request *http.Request, limit int64) ([]byte, int64, error) {
	if request.Body == nil || request.Body == http.NoBody {
		return nil, 0, nil
	}
	reader := &budgetedReader{source: io.LimitReader(request.Body, limit+1), budget: p.budget}
	buffered, err := io.ReadAll(reader)
	if err != nil {
		p.budget.give(reader.taken)
		return nil, 0, err
	}
	if int64(len(buffered)) > limit {
		p.budget.give(reader.taken)
		return nil, 0, errBodyTooLarge
	}
	return buffered, reader.taken, nil
}
