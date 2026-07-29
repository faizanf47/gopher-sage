package profile_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faizanf47/gopher-sage/internal/profile"
)

func TestHTTPFetcher_servesFixture(t *testing.T) {
	t.Parallel()

	fixture := mustReadFixture(t, "cpu.pb.gz")

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	got, err := profile.NewHTTPFetcher().Fetch(context.Background(), profile.Source{
		BaseURL: srv.URL,
		Type:    profile.TypeCPU,
		Seconds: 5,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got) != len(fixture) {
		t.Fatalf("body length %d, want %d", len(got), len(fixture))
	}
	if !strings.HasSuffix(gotPath, "/debug/pprof/profile") {
		t.Fatalf("server saw path %q, want suffix /debug/pprof/profile", gotPath)
	}
}

func TestHTTPFetcher_propagatesHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := profile.NewHTTPFetcher().Fetch(context.Background(), profile.Source{
		BaseURL: srv.URL,
		Type:    profile.TypeHeap,
	})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("want status-500 error, got %v", err)
	}
}

func TestHTTPFetcher_respectsContextCancellation(t *testing.T) {
	t.Parallel()

	// The handler hangs until the request context fires.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := profile.NewHTTPFetcher().Fetch(ctx, profile.Source{
		BaseURL: srv.URL,
		Type:    profile.TypeHeap,
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// mustReadFixture reads a profile fixture relative to the repo
// root. If the fixture is absent (the captured profiles are gitignored
// to keep the tree small) the test is skipped rather than failed —
// the layers exercised by these fixtures are also exercised by the
// pure-parser unit tests, which run without external data.
func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	root := repoRoot(t)
	p := filepath.Join(root, "fixtures", "profiles", name)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("profile fixture %s is not present; skipping", p)
		}
		t.Fatalf("read fixture %s: %v", p, err)
	}
	return raw
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod from cwd")
		}
		dir = parent
	}
}
