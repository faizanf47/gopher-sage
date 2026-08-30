package analyze

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faizanf47/gopher-sage/internal/profile"
)

func TestCapture_writesValidatedProfiles(t *testing.T) {
	t.Parallel()

	cpuRaw := marshalProfile(t, heavyJSONCPUProfile())
	heapRaw := marshalProfile(t, heapProfileWithBufferGrowth())
	fetcher := stubFetcher{byType: map[profile.Type][]byte{
		profile.TypeCPU:  cpuRaw,
		profile.TypeHeap: heapRaw,
	}}
	dir := t.TempDir()

	files, err := Capture(context.Background(), fetcher, CaptureOptions{
		Server: "http://localhost:6060",
		Types:  []profile.Type{profile.TypeCPU, profile.TypeHeap},
		OutDir: dir,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}

	want := []struct {
		typ  profile.Type
		name string
		size int
	}{
		{profile.TypeCPU, "cpu.pb.gz", len(cpuRaw)},
		{profile.TypeHeap, "heap.pb.gz", len(heapRaw)},
	}
	for i, w := range want {
		f := files[i]
		if f.Type != w.typ {
			t.Errorf("files[%d].Type = %q, want %q", i, f.Type, w.typ)
		}
		if f.Path != filepath.Join(dir, w.name) {
			t.Errorf("files[%d].Path = %q, want %q", i, f.Path, filepath.Join(dir, w.name))
		}
		if f.Bytes != w.size {
			t.Errorf("files[%d].Bytes = %d, want %d", i, f.Bytes, w.size)
		}
		onDisk, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatalf("read %s: %v", f.Path, err)
		}
		if len(onDisk) != w.size {
			t.Errorf("%s: %d bytes on disk, want %d", f.Path, len(onDisk), w.size)
		}
	}
}

func TestCapture_goroutineProfile(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(realFixture(t, "goroutine.pb.gz"))
	if err != nil {
		t.Fatalf("read goroutine fixture: %v", err)
	}
	fetcher := stubFetcher{byType: map[profile.Type][]byte{profile.TypeGoroutine: raw}}
	dir := t.TempDir()

	files, err := Capture(context.Background(), fetcher, CaptureOptions{
		Server: "http://localhost:6060",
		Types:  []profile.Type{profile.TypeGoroutine},
		OutDir: dir,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(dir, "goroutine.pb.gz") {
		t.Errorf("files = %+v, want one goroutine.pb.gz", files)
	}
}

func TestCapture_overwritesExistingFile(t *testing.T) {
	t.Parallel()

	raw := marshalProfile(t, heavyJSONCPUProfile())
	fetcher := stubFetcher{byType: map[profile.Type][]byte{profile.TypeCPU: raw}}
	dir := t.TempDir()
	stale := filepath.Join(dir, "cpu.pb.gz")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}

	if _, err := Capture(context.Background(), fetcher, CaptureOptions{
		Server: "http://localhost:6060",
		Types:  []profile.Type{profile.TypeCPU},
		OutDir: dir,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	onDisk, err := os.ReadFile(stale)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(onDisk) != len(raw) {
		t.Errorf("stale file not overwritten: %d bytes, want %d", len(onDisk), len(raw))
	}
}

func TestCapture_rejectsNonProfileResponse(t *testing.T) {
	t.Parallel()

	fetcher := stubFetcher{byType: map[profile.Type][]byte{
		profile.TypeCPU: []byte("<html>404 not found</html>"),
	}}
	dir := t.TempDir()

	_, err := Capture(context.Background(), fetcher, CaptureOptions{
		Server: "http://localhost:6060",
		Types:  []profile.Type{profile.TypeCPU},
		OutDir: dir,
	})
	if err == nil {
		t.Fatal("Capture: want error for non-profile bytes, got nil")
	}
	if !strings.Contains(err.Error(), "net/http/pprof") {
		t.Errorf("error %q should hint at the pprof mount", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "cpu.pb.gz")); !os.IsNotExist(statErr) {
		t.Error("poisoned cpu.pb.gz was written despite validation failure")
	}
}

func TestCapture_contextCancellation(t *testing.T) {
	t.Parallel()

	fetcher := stubFetcher{err: context.Canceled}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Capture(ctx, fetcher, CaptureOptions{
		Server: "http://localhost:6060",
		Types:  []profile.Type{profile.TypeHeap},
		OutDir: t.TempDir(),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Capture: err = %v, want context.Canceled", err)
	}
}

func TestCapture_validation(t *testing.T) {
	t.Parallel()

	valid := CaptureOptions{
		Server: "http://localhost:6060",
		Types:  []profile.Type{profile.TypeCPU},
		OutDir: "out",
	}
	tests := []struct {
		name    string
		mutate  func(*CaptureOptions)
		wantSub string
	}{
		{"missing server", func(o *CaptureOptions) { o.Server = "" }, "server URL"},
		{"no types", func(o *CaptureOptions) { o.Types = nil }, "at least one"},
		{"unsupported type", func(o *CaptureOptions) { o.Types = []profile.Type{profile.Type("wat")} }, "unsupported"},
		{"negative seconds", func(o *CaptureOptions) { o.Seconds = -1 }, "non-negative"},
		{"missing out dir", func(o *CaptureOptions) { o.OutDir = "" }, "output directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := valid
			tt.mutate(&opts)
			_, err := Capture(context.Background(), stubFetcher{}, opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("err = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}
