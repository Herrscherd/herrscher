package usage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PricesURL is the LiteLLM price list the cost of the transcripts is computed from.
const PricesURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// PricesTTL is how long a downloaded price list is used before it is fetched again.
const PricesTTL = 24 * time.Hour

// ErrPricesStatus reports a price list download answered with a status other than 200.
var ErrPricesStatus = errors.New("usage prices: unexpected status")

const (
	pricesRetry    = time.Minute
	pricesTimeout  = 30 * time.Second
	maxPricesBytes = 32 << 20
)

// PriceSource keeps the LiteLLM price list in a file of the state directory, and fetches
// it again once it is older than PricesTTL. A failed fetch keeps the stale list.
type PriceSource struct {
	Path   string
	URL    string
	Client *http.Client
	Now    func() time.Time

	mu        sync.Mutex
	book      PriceBook
	fetchedAt time.Time
	triedAt   time.Time
}

// Book is the price list last loaded, empty until the first load.
func (s *PriceSource) Book() PriceBook {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.book
}

// Refresh loads the list from its file when none is loaded yet, then downloads it when
// it is stale and the last attempt is more than a minute old. The download runs without
// the lock, so Book keeps answering meanwhile.
func (s *PriceSource) Refresh(ctx context.Context) error {
	if !s.due() {
		return nil
	}
	raw, err := s.download(ctx)
	if err != nil {
		return err
	}
	book, err := ParsePrices(raw)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.book, s.fetchedAt = book, s.Now()
	s.mu.Unlock()
	return writeAtomic(s.Path, raw)
}

func (s *PriceSource) due() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.book.Empty() {
		s.loadFile()
	}
	now := s.Now()
	if now.Sub(s.fetchedAt) < PricesTTL || now.Sub(s.triedAt) < pricesRetry {
		return false
	}
	s.triedAt = now
	return true
}

func (s *PriceSource) loadFile() {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return
	}
	book, err := ParsePrices(raw)
	if err != nil {
		return
	}
	info, err := os.Stat(s.Path)
	if err != nil {
		return
	}
	s.book, s.fetchedAt = book, info.ModTime()
}

func (s *PriceSource) download(ctx context.Context) ([]byte, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, pricesTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, s.URL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("usage prices: %w", err)
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("usage prices: %w", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxPricesBytes))
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return nil, fmt.Errorf("usage prices: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w %d", ErrPricesStatus, response.StatusCode)
	}
	return raw, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("usage cache: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("usage cache: %w", err)
	}
	_, writeErr := temp.Write(data)
	if err := errors.Join(writeErr, temp.Chmod(fileMode), temp.Close()); err != nil {
		return errors.Join(fmt.Errorf("usage cache: %w", err), removeMissing(temp.Name()))
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return errors.Join(fmt.Errorf("usage cache: %w", err), removeMissing(temp.Name()))
	}
	return nil
}

func removeMissing(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("usage cache: %w", err)
	}
	return nil
}
