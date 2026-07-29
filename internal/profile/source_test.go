package profile

import (
	"strings"
	"testing"
)

func TestSource_URL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  Source
		want string
	}{
		{
			name: "bare host adds debug path",
			src:  Source{BaseURL: "http://localhost:6060", Type: TypeHeap},
			want: "http://localhost:6060/debug/pprof/heap",
		},
		{
			name: "trailing slash on host",
			src:  Source{BaseURL: "http://localhost:6060/", Type: TypeHeap},
			want: "http://localhost:6060/debug/pprof/heap",
		},
		{
			name: "already includes debug/pprof",
			src:  Source{BaseURL: "http://localhost:6060/debug/pprof", Type: TypeAllocs},
			want: "http://localhost:6060/debug/pprof/allocs",
		},
		{
			name: "already includes debug/pprof/",
			src:  Source{BaseURL: "http://localhost:6060/debug/pprof/", Type: TypeGoroutine},
			want: "http://localhost:6060/debug/pprof/goroutine",
		},
		{
			name: "CPU maps to profile endpoint",
			src:  Source{BaseURL: "http://localhost:6060", Type: TypeCPU},
			want: "http://localhost:6060/debug/pprof/profile",
		},
		{
			name: "CPU with seconds adds query",
			src:  Source{BaseURL: "http://localhost:6060", Type: TypeCPU, Seconds: 10},
			want: "http://localhost:6060/debug/pprof/profile?seconds=10",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.src.URL()
			if err != nil {
				t.Fatalf("URL: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSource_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		src     Source
		wantErr string
	}{
		{name: "missing url", src: Source{Type: TypeCPU}, wantErr: "BaseURL is required"},
		{name: "unknown type", src: Source{BaseURL: "http://x", Type: "wat"}, wantErr: "unknown type"},
		{name: "negative seconds", src: Source{BaseURL: "http://x", Type: TypeCPU, Seconds: -1}, wantErr: "non-negative"},
		{name: "ok", src: Source{BaseURL: "http://x", Type: TypeCPU}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.src.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestType_Valid(t *testing.T) {
	t.Parallel()
	for _, ty := range AllTypes() {
		if !ty.Valid() {
			t.Fatalf("%q should be valid", ty)
		}
	}
	if Type("nope").Valid() {
		t.Fatal("garbage type reported valid")
	}
}
