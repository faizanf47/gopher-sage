package profile

import (
	"net/url"
	"strings"
	"testing"
)

// FuzzSourceURL checks the invariant URL() promises for every input
// Validate accepts: the result re-parses as an http(s) URL whose
// path routes through /debug/pprof/ to the CPU endpoint, carrying
// the seconds parameter when one was set.
func FuzzSourceURL(f *testing.F) {
	f.Add("http://localhost:6060", 30)
	f.Add("http://localhost:6060/", 0)
	f.Add("http://host/debug/pprof", 10)
	f.Add("https://host:9090/debug/pprof/", 5)
	f.Add("http://host/app//", 7)
	f.Add("localhost:6060", 30)
	f.Add("://bad", 1)
	f.Add("http://host/path?x=1#frag", 2)

	f.Fuzz(func(t *testing.T, base string, seconds int) {
		src := Source{BaseURL: base, Type: TypeCPU, Seconds: seconds}
		got, err := src.URL()
		if err != nil {
			return // rejected inputs are fine; we check accepted ones
		}
		u, perr := url.Parse(got)
		if perr != nil {
			t.Fatalf("URL(%q) = %q, which does not re-parse: %v", base, got, perr)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			t.Errorf("URL(%q) = %q, scheme %q escaped validation", base, got, u.Scheme)
		}
		if !strings.Contains(u.Path, "/debug/pprof/") {
			t.Errorf("URL(%q) = %q, path %q misses /debug/pprof/", base, got, u.Path)
		}
		if !strings.HasSuffix(u.Path, TypeCPU.Endpoint()) {
			t.Errorf("URL(%q) = %q, path %q misses the %q endpoint", base, got, u.Path, TypeCPU.Endpoint())
		}
		if seconds > 0 && u.Query().Get("seconds") == "" {
			t.Errorf("URL(%q) = %q dropped the seconds parameter", base, got)
		}
	})
}
