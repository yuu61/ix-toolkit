package infrastructure

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

type fetchResponse struct {
	body, etag string
	status     int
}

type fetchSite struct {
	*httptest.Server
	mu       sync.Mutex
	files    map[string]fetchResponse
	requests map[string][]http.Header
}

func newFetchSite(t *testing.T) *fetchSite {
	t.Helper()
	s := &fetchSite{
		files: map[string]fetchResponse{
			"/":                    {body: "<title>Example manual 1.5a版</title>"},
			"/searchindex.js":      {body: `Search.setIndex({"docnames":["index","one","two"]})`},
			"/one.html":            {body: `<h1>1. Example</h1><img src="_images/diagram.svg">`, etag: `"one"`},
			"/two.html":            {body: "<h1>2. Example</h1>", etag: `"two"`},
			"/_images/diagram.svg": {body: "<svg></svg>", etag: `"image"`},
		},
		requests: map[string][]http.Header{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		f, ok := s.files[r.URL.Path]
		s.requests[r.URL.Path] = append(s.requests[r.URL.Path], r.Header.Clone())
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		if f.etag != "" {
			w.Header().Set("ETag", f.etag)
			w.Header().Set("Last-Modified", "Mon, 01 Jun 2026 00:00:00 GMT")
			if r.Header.Get("If-None-Match") == f.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		_, _ = io.WriteString(w, f.body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *fetchSite) set(path string, f fetchResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = f
}

func (s *fetchSite) lastRequest(path string) http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.requests[path]
	if len(r) == 0 {
		return nil
	}
	return r[len(r)-1]
}

func (s *fetchSite) doc() domain.Doc {
	return domain.Doc{Name: "manual", Kind: "web", Version: "1.5a", URL: s.URL}
}

func (s *fetchSite) fetch(cache string, force bool) error {
	return FetchDoc(io.Discard, s.Client(), s.doc(), cache, force, 0, "test")
}

func readFetchFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFetchWebConditionalCache(t *testing.T) {
	s := newFetchSite(t)
	cache := t.TempDir()
	dst := CachePath(cache, s.doc())
	if err := s.fetch(cache, false); err != nil {
		t.Fatal(err)
	}
	original := readFetchFile(t, filepath.Join(dst, "one.html"))
	if err := s.fetch(cache, false); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"one.html", "two.html", "_images/diagram.svg"} {
		if s.lastRequest("/"+rel).Get("If-None-Match") == "" {
			t.Errorf("%s: existing cache was not used", rel)
		}
	}
	if got := readFetchFile(t, filepath.Join(dst, "one.html")); got != original {
		t.Errorf("304 lost body: %q", got)
	}
	if !WebFetched(dst, s.doc()) {
		t.Error("complete cache was not recognized")
	}
}

func TestFetchWebRepairsMissingFiles(t *testing.T) {
	for _, rel := range []string{"one.html", "_images/diagram.svg"} {
		t.Run(rel, func(t *testing.T) {
			s := newFetchSite(t)
			cache := t.TempDir()
			dst := CachePath(cache, s.doc())
			if err := s.fetch(cache, false); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dst, filepath.FromSlash(rel))
			original := readFetchFile(t, file)
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if WebFetched(dst, s.doc()) {
				t.Fatal("missing file considered complete")
			}
			if err := s.fetch(cache, false); err != nil {
				t.Fatal(err)
			}
			h := s.lastRequest("/" + rel)
			if h.Get("If-None-Match") != "" || h.Get("If-Modified-Since") != "" {
				t.Error("missing body used conditional GET")
			}
			if got := readFetchFile(t, file); got != original {
				t.Errorf("body = %q, want %q", got, original)
			}
			if !WebFetched(dst, s.doc()) {
				t.Error("repaired cache considered incomplete")
			}
		})
	}
}

func TestFetchWebFailurePreservesPreviousFiles(t *testing.T) {
	for _, failPath := range []string{"/two.html", "/_images/diagram.svg"} {
		t.Run(failPath, func(t *testing.T) {
			s := newFetchSite(t)
			cache := t.TempDir()
			dst := CachePath(cache, s.doc())
			if err := s.fetch(cache, false); err != nil {
				t.Fatal(err)
			}
			original := map[string]string{}
			for _, rel := range []string{"index.html", "one.html", "two.html", "_images/diagram.svg", etagFile} {
				original[rel] = readFetchFile(t, filepath.Join(dst, filepath.FromSlash(rel)))
			}
			const newBody = `new body<img src="_images/diagram.svg">`
			s.set("/one.html", fetchResponse{body: newBody, etag: `"new"`})
			s.set(failPath, fetchResponse{status: http.StatusServiceUnavailable})
			if err := s.fetch(cache, true); err == nil {
				t.Fatal("failed download returned success")
			}
			if WebFetched(dst, s.doc()) {
				t.Error("failed refresh considered complete")
			}
			for rel, want := range original {
				if got := readFetchFile(t, filepath.Join(dst, filepath.FromSlash(rel))); got != want {
					t.Errorf("failed refresh changed %s", rel)
				}
			}
			files, err := os.ReadDir(cache)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 {
				t.Errorf("temporary directories remain: %v", files)
			}
			s.set(failPath, fetchResponse{body: "restored", etag: `"restored"`})
			if err := s.fetch(cache, false); err != nil {
				t.Fatal(err)
			}
			if !WebFetched(dst, s.doc()) {
				t.Error("retry did not complete cache")
			}
			if got := readFetchFile(t, filepath.Join(dst, "one.html")); got != newBody {
				t.Errorf("retry body = %q", got)
			}
		})
	}
}

func TestFetchWebRejectsUnexpectedNotModified(t *testing.T) {
	s := newFetchSite(t)
	s.set("/one.html", fetchResponse{status: http.StatusNotModified})
	cache := t.TempDir()
	if err := s.fetch(cache, false); err == nil || !strings.Contains(err.Error(), "304") {
		t.Fatalf("unexpected 304 error = %v", err)
	}
	if WebFetched(CachePath(cache, s.doc()), s.doc()) {
		t.Error("missing body considered complete")
	}
}

func TestFetchWebRejectsDifferentVersion(t *testing.T) {
	s := newFetchSite(t)
	s.set("/", fetchResponse{body: "<title>Example manual 1.5ab版</title>"})
	if err := s.fetch(t.TempDir(), false); err == nil {
		t.Fatal("different version accepted")
	}
	if s.lastRequest("/searchindex.js") != nil {
		t.Error("fetched pages before validating version")
	}
}

func TestFetchWebRejectsNamesOutsideDocumentCache(t *testing.T) {
	s := newFetchSite(t)
	for _, name := range []string{"", ".", "..", "../other", "nested/manual", `nested\manual`} {
		d := s.doc()
		d.Name = name
		if err := FetchDoc(io.Discard, s.Client(), d, t.TempDir(), false, 0, "test"); err == nil {
			t.Errorf("unsafe cache name %q accepted", name)
		}
	}
	if s.lastRequest("/") != nil {
		t.Error("fetch began before validating cache name")
	}
}

func TestPublishWebCacheRestoresOnFailure(t *testing.T) {
	root := t.TempDir()
	dst, previous := filepath.Join(root, "cache"), filepath.Join(root, "previous")
	if err := os.Mkdir(dst, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "page.html"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishWebCache(filepath.Join(root, "missing"), dst, previous); err == nil {
		t.Fatal("missing stage succeeded")
	}
	if got := readFetchFile(t, filepath.Join(dst, "page.html")); got != "old" {
		t.Errorf("restored body = %q", got)
	}
}
