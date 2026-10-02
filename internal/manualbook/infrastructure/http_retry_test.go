package infrastructure

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

const (
	retryPDFName = "sample"
	completeBody = "complete"
)

func TestRetryDelay(t *testing.T) {
	for _, tc := range []struct {
		header          string
		minimum, want   time.Duration
		status, attempt int
		fail            bool
	}{
		{"", 0, 2 * time.Second, 500, 0, false},
		{"", 0, 4 * time.Second, 500, 1, false},
		{"0", time.Second, time.Second, 429, 0, false},
		{"8", 0, 8 * time.Second, 503, 0, false},
		{"31", 0, 0, 503, 0, true},
		{time.Now().Add(time.Minute).UTC().Format(http.TimeFormat), 0, 0, 429, 0, true},
		{"invalid", 0, 2 * time.Second, 429, 0, false},
		{"-1", 0, 2 * time.Second, 429, 0, false},
	} {
		t.Run(strconv.Itoa(tc.status)+"/"+tc.header, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": {tc.header}}}
			got, err := retryDelay(resp, tc.attempt, tc.minimum)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("delay=%s, error=%v", got, err)
			}
		})
	}
}

func TestGETRetriesAndPermanentFailures(t *testing.T) {
	for _, status := range []int{429, 503, 403, 404} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
			}))
			defer s.Close()
			d := domain.Doc{Name: retryPDFName, Kind: domain.KindPDF, URL: s.URL}
			err := FetchDoc(t.Context(), io.Discard, s.Client(), d, t.TempDir(), true, 0, "test")
			want := int32(1)
			if retryStatus(status) {
				want = 3
			}
			if err == nil || calls.Load() != want {
				t.Fatalf("calls=%d, error=%v", calls.Load(), err)
			}
		})
	}
}

func TestPDFRetriesTruncatedBodyBeforePublishing(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Length", "100")
			_, _ = io.WriteString(w, "partial") //nolint:errcheck // Deliberately truncated response.
			return
		}
		_, _ = io.WriteString(w, completeBody) //nolint:errcheck // Simulated server.
	}))
	defer s.Close()
	cache := t.TempDir()
	d := domain.Doc{Name: retryPDFName, Kind: domain.KindPDF, URL: s.URL}
	if err := FetchDoc(t.Context(), io.Discard, s.Client(), d, cache, true, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if got := readFetchFile(t, CachePath(cache, d)); got != completeBody || calls.Load() != 2 {
		t.Fatalf("content=%q, calls=%d", got, calls.Load())
	}
	assertNoFetchStage(t, cache)
}

func assertNoFetchStage(t *testing.T, cache string) {
	t.Helper()
	for _, pattern := range []string{"*.part", ".manualbook-*"} {
		files, err := filepath.Glob(filepath.Join(cache, pattern))
		if err != nil || len(files) != 0 {
			t.Fatalf("stage files=%v, error=%v", files, err)
		}
	}
}

func TestPDFCancellationPreservesPreviousAndRemovesPartial(t *testing.T) {
	started := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = io.WriteString(w, "partial") //nolint:errcheck // Client cancellation is expected.
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		close(started)
		<-r.Context().Done()
	}))
	defer s.Close()
	cache := t.TempDir()
	d := domain.Doc{Name: retryPDFName, Kind: domain.KindPDF, URL: s.URL}
	if err := os.WriteFile(CachePath(cache, d), []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- FetchDoc(ctx, io.Discard, s.Client(), d, cache, true, 0, "test") }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	if got := readFetchFile(t, CachePath(cache, d)); got != "previous" {
		t.Fatal(got)
	}
	assertNoFetchStage(t, cache)
}

func TestRetryAndRequestDelaysAreCancellable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := waitContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	started := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		close(started)
	}))
	defer s.Close()
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		d := domain.Doc{Name: retryPDFName, Kind: domain.KindPDF, URL: s.URL}
		done <- FetchDoc(ctx, io.Discard, s.Client(), d, t.TempDir(), true, 0, "test")
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRetryClassifiesRemoteAndLocalFailures(t *testing.T) {
	if !retryableNetwork(responseReadError{io.ErrUnexpectedEOF}) || !retryableNetwork(&net.OpError{Op: "read", Err: io.ErrUnexpectedEOF}) {
		t.Fatal("remote error must retry")
	}
	for _, err := range []error{&os.PathError{Op: "write", Path: "local", Err: errors.New("disk full")}, x509.UnknownAuthorityError{}, x509.HostnameError{}, &net.DNSError{IsNotFound: true}} {
		if retryableNetwork(err) {
			t.Fatalf("must not retry: %v", err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebCancellationPreservesBodyAndInvalidatesCompletion(t *testing.T) {
	s := newFetchSite(t)
	cache := t.TempDir()
	if err := s.fetch(cache, false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := s.Client()
	transport := client.Transport
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, firstPage) {
			cancel()
			return nil, ctx.Err()
		}
		return transport.RoundTrip(r)
	})
	err := FetchDoc(ctx, io.Discard, client, s.doc(), cache, true, 0, "test")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	dst := CachePath(cache, s.doc())
	if got := readFetchFile(t, filepath.Join(dst, firstPage)); !strings.Contains(got, "Example") {
		t.Fatal(got)
	}
	if WebFetched(dst, s.doc()) {
		t.Fatal("failed refresh must not be marked complete")
	}
	assertNoFetchStage(t, cache)
}

func TestWebBodyRetryDoesNotPublishPartialETag(t *testing.T) {
	s := newFetchSite(t)
	cache := t.TempDir()
	if err := s.fetch(cache, false); err != nil {
		t.Fatal(err)
	}
	client := s.Client()
	transport := client.Transport
	var calls atomic.Int32
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, firstPage) && calls.Add(1) == 1 {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"ETag": {"partial"}}, Body: io.NopCloser(&brokenBody{})}, nil
		}
		return transport.RoundTrip(r)
	})
	s.set("/one.html", fetchResponse{body: completeBody, etag: "complete-tag"})
	if err := FetchDoc(t.Context(), io.Discard, client, s.doc(), cache, true, 0, "test"); err != nil {
		t.Fatal(err)
	}
	dst := CachePath(cache, s.doc())
	if calls.Load() != 2 || readFetchFile(t, filepath.Join(dst, firstPage)) != completeBody || readETags(dst)[firstPage].ETag != "complete-tag" {
		t.Fatal("partial body or ETag was published")
	}
	assertNoFetchStage(t, cache)
}

type brokenBody struct{ read bool }

func (b *brokenBody) Read(p []byte) (int, error) {
	if !b.read {
		b.read = true
		return copy(p, "partial"), nil
	}
	return 0, io.ErrUnexpectedEOF
}
