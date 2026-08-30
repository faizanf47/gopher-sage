// Package profile identifies and fetches Go pprof profiles from a
// running server's net/http/pprof endpoint: typed profile kinds, URL
// construction from any reasonable base-address spelling, and an
// HTTP fetcher that returns the raw protobuf bytes.
package profile

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Type names the kind of profile to fetch. The values match the
// last path segment of the standard `net/http/pprof` handler URLs
// so they double as human-friendly identifiers.
type Type string

// The profile types the standard net/http/pprof handler serves.
const (
	TypeCPU          Type = "cpu"
	TypeHeap         Type = "heap"
	TypeAllocs       Type = "allocs"
	TypeGoroutine    Type = "goroutine"
	TypeBlock        Type = "block"
	TypeMutex        Type = "mutex"
	TypeThreadCreate Type = "threadcreate"
)

// AllTypes returns every supported Type in canonical order. Useful
// for help text and validation messages.
func AllTypes() []Type {
	return []Type{
		TypeCPU, TypeHeap, TypeAllocs,
		TypeGoroutine, TypeBlock, TypeMutex, TypeThreadCreate,
	}
}

// Valid reports whether t is one of the known profile types.
func (t Type) Valid() bool {
	return slices.Contains(AllTypes(), t)
}

// Endpoint returns the path segment under `/debug/pprof/` that
// serves this profile type. For CPU it differs from the type name
// itself (the runtime exposes CPU profiles under `profile`).
func (t Type) Endpoint() string {
	if t == TypeCPU {
		return "profile"
	}
	return string(t)
}

// Source identifies a single profile to fetch from a running
// server. BaseURL may be any of the following equivalent forms:
//
//	http://host:6060
//	http://host:6060/
//	http://host:6060/debug/pprof
//	http://host:6060/debug/pprof/
//
// Seconds applies to TypeCPU only (the runtime samples for that
// long before responding). Zero means "use the server default".
type Source struct {
	BaseURL string
	Type    Type
	Seconds int
}

// Validate enforces the invariants Source assumes elsewhere in the
// package: an absolute http(s) URL with a host, a known Type, and a
// non-negative duration. Requiring the scheme up front catches
// spellings like "localhost:6060", which url.Parse reads as an
// opaque URL with scheme "localhost" and URL() would otherwise turn
// into a fetch target with no pprof path at all.
func (s Source) Validate() error {
	if strings.TrimSpace(s.BaseURL) == "" {
		return fmt.Errorf("profile: BaseURL is required")
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return fmt.Errorf("profile: parse BaseURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("profile: BaseURL must be an http or https URL, got %q", s.BaseURL)
	}
	if u.Host == "" {
		return fmt.Errorf("profile: BaseURL %q has no host", s.BaseURL)
	}
	if !s.Type.Valid() {
		return fmt.Errorf("profile: unknown type %q (want one of %v)", s.Type, AllTypes())
	}
	if s.Seconds < 0 {
		return fmt.Errorf("profile: Seconds must be non-negative, got %d", s.Seconds)
	}
	return nil
}

// URL constructs the absolute HTTP URL the runtime serves this
// profile from. The result includes the `seconds` query parameter
// when applicable.
func (s Source) URL() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return "", fmt.Errorf("parse BaseURL: %w", err)
	}
	u.Path = normaliseDebugPath(u.Path) + s.Type.Endpoint()
	if s.Type == TypeCPU && s.Seconds > 0 {
		q := u.Query()
		q.Set("seconds", strconv.Itoa(s.Seconds))
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// normaliseDebugPath returns a path that is guaranteed to end in
// "/debug/pprof/" so the caller can append the endpoint segment.
func normaliseDebugPath(p string) string {
	if p == "" {
		return "/debug/pprof/"
	}
	trimmed := strings.TrimRight(p, "/")
	if strings.HasSuffix(trimmed, "/debug/pprof") {
		return trimmed + "/"
	}
	if trimmed == "" {
		return "/debug/pprof/"
	}
	return trimmed + "/debug/pprof/"
}
