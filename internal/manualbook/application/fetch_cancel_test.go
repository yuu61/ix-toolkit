package application

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

func TestBackgroundFetchJoinsAndDoesNotStartNextDocument(t *testing.T) { //nolint:cyclop // One lifecycle test covers request, cancellation, join, and cleanup.
	started := make(chan struct{})
	var indexes atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "searchindex.js"):
			_, _ = io.WriteString(w, `Search.setIndex({"docnames":["one"]})`) //nolint:errcheck // Simulated server.
		case strings.HasSuffix(r.URL.Path, "one.html"):
			w.Header().Set("Content-Length", "100000")
			_, _ = io.WriteString(w, "partial") //nolint:errcheck // Cancellation is expected.
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Error(err)
			}
			close(started)
			<-r.Context().Done()
		default:
			indexes.Add(1)
			_, _ = io.WriteString(w, "<title>Example 1.5a</title>") //nolint:errcheck // Simulated server.
		}
	}))
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cache := t.TempDir()
	docs := []domain.Doc{
		{Name: "first", Kind: domain.KindWeb, Version: "1.5a", URL: s.URL + "/first/"},
		{Name: "second", Kind: domain.KindWeb, Version: "1.5a", URL: s.URL + "/second/"},
	}
	results, done := startWebFetch(ctx, s.Client(), docs, cache, false, false)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background fetch did not finish")
	}
	for _, result := range results {
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if indexes.Load() != 1 {
		t.Fatal("started another document after cancellation")
	}
	stages, err := filepath.Glob(filepath.Join(cache, ".manualbook-*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("stage files=%v, error=%v", stages, err)
	}
}
