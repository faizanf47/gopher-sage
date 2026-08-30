package analyze

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// writeProfileFile serialises a synthetic profile into a temp file
// and returns its path.
func writeProfileFile(t *testing.T, name string, raw []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatalf("write profile file: %v", err)
	}
	return p
}

func TestRunFiles_inferKindAndReport(t *testing.T) {
	t.Parallel()

	cpuProf := heavyJSONCPUProfile()
	cpuProf.DurationNanos = 20_000_000_000
	cpuPath := writeProfileFile(t, "cpu.pb.gz", marshalProfile(t, cpuProf))

	// The shared heap fixture only populates the alloc columns; give
	// it live memory too so the inuse_space-based top table (and any
	// inuse detector) has data to rank.
	heapProf := heapProfileWithBufferGrowth()
	for _, s := range heapProf.Sample {
		s.Value[2] = s.Value[0] // inuse_objects = alloc_objects
		s.Value[3] = s.Value[1] // inuse_space = alloc_space
	}
	heapPath := writeProfileFile(t, "heap.pb.gz", marshalProfile(t, heapProf))

	rep, err := RunFiles(FileOptions{Paths: []string{cpuPath, heapPath}, TopN: 3})
	if err != nil {
		t.Fatalf("RunFiles: %v", err)
	}

	if rep.Server != "" {
		t.Errorf("Server = %q, want empty for file analysis", rep.Server)
	}
	if len(rep.Profiles) != 2 {
		t.Fatalf("len(Profiles) = %d, want 2", len(rep.Profiles))
	}

	cpu, heap := rep.Profiles[0], rep.Profiles[1]
	if cpu.Type != profile.TypeCPU {
		t.Errorf("Profiles[0].Type = %q, want %q (inferred from sample types)", cpu.Type, profile.TypeCPU)
	}
	if heap.Type != profile.TypeHeap {
		t.Errorf("Profiles[1].Type = %q, want %q (inferred from sample types)", heap.Type, profile.TypeHeap)
	}
	if cpu.Source != cpuPath {
		t.Errorf("Profiles[0].Source = %q, want %q", cpu.Source, cpuPath)
	}
	if cpu.Bytes == 0 {
		t.Error("Profiles[0].Bytes = 0, want the file size")
	}
	if len(cpu.Findings) == 0 {
		t.Error("CPU profile produced no findings, want the JSON detector to fire")
	}
	wantTotals := []profanalyze.SampleTotal{{SampleType: "cpu", Unit: "nanoseconds", Total: 1000}}
	if !reflect.DeepEqual(cpu.Totals, wantTotals) {
		t.Errorf("Profiles[0].Totals = %+v, want %+v", cpu.Totals, wantTotals)
	}
	if cpu.DurationNanos != 20_000_000_000 {
		t.Errorf("Profiles[0].DurationNanos = %d, want 20s", cpu.DurationNanos)
	}

	for i, pr := range rep.Profiles {
		if pr.Top == nil {
			t.Fatalf("Profiles[%d].Top = nil, want a top report when TopN > 0", i)
		}
		if got := len(pr.Top.Entries); got == 0 || got > 3 {
			t.Errorf("Profiles[%d]: len(Top.Entries) = %d, want 1..3", i, got)
		}
	}
}

func TestRunFiles_errors(t *testing.T) {
	t.Parallel()

	goodCPU := writeProfileFile(t, "cpu.pb.gz", marshalProfile(t, heavyJSONCPUProfile()))
	garbage := writeProfileFile(t, "garbage.pb.gz", []byte("not a profile"))

	tests := []struct {
		name    string
		opts    FileOptions
		wantSub string
	}{
		{
			name:    "no paths",
			opts:    FileOptions{},
			wantSub: "at least one profile file",
		},
		{
			name:    "negative min share",
			opts:    FileOptions{Paths: []string{goodCPU}, MinShare: -1},
			wantSub: "min share",
		},
		{
			name:    "negative top",
			opts:    FileOptions{Paths: []string{goodCPU}, TopN: -1},
			wantSub: "top N",
		},
		{
			name:    "missing file",
			opts:    FileOptions{Paths: []string{filepath.Join(t.TempDir(), "absent.pb.gz")}},
			wantSub: "absent.pb.gz",
		},
		{
			name:    "unparseable file",
			opts:    FileOptions{Paths: []string{garbage}},
			wantSub: "parse",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := RunFiles(tt.opts)
			if err == nil {
				t.Fatal("RunFiles: want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not mention %q", err, tt.wantSub)
			}
		})
	}
}

// realFixture returns the path of a captured profile fixture,
// skipping the test when it is absent from the checkout.
func realFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "fixtures", "profiles", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("profile fixture %s is not present; skipping", p)
	}
	return p
}

func TestRunFiles_lenient(t *testing.T) {
	t.Parallel()

	cpuPath := writeProfileFile(t, "cpu.pb.gz", marshalProfile(t, heavyJSONCPUProfile()))
	goroutinePath := realFixture(t, "goroutine.pb.gz")
	garbage := writeProfileFile(t, "garbage.pb.gz", []byte("not a profile"))
	paths := []string{cpuPath, goroutinePath, garbage}

	// Strict mode aborts on the first unusable file.
	if _, err := RunFiles(FileOptions{Paths: paths}); err == nil {
		t.Fatal("strict RunFiles: want error, got nil")
	}

	rep, err := RunFiles(FileOptions{Paths: paths, Lenient: true})
	if err != nil {
		t.Fatalf("lenient RunFiles: %v", err)
	}
	if len(rep.Profiles) != 2 {
		t.Fatalf("len(Profiles) = %d, want 2 (cpu + goroutine summary)", len(rep.Profiles))
	}
	if !reflect.DeepEqual(rep.Skipped, []string{garbage}) {
		t.Errorf("Skipped = %v, want just the garbage file", rep.Skipped)
	}

	g := rep.Profiles[1]
	if g.Type != profile.TypeGoroutine || !g.Summary {
		t.Errorf("Profiles[1] = type %q summary %v, want goroutine summary", g.Type, g.Summary)
	}
	if len(g.Findings) != 0 {
		t.Errorf("summary profile carries findings: %+v", g.Findings)
	}
	if g.Top == nil || len(g.Top.Entries) == 0 {
		t.Error("summary profile is missing its top-frames table")
	}
	if len(g.Totals) == 0 || g.Totals[0].SampleType != "goroutine" || g.Totals[0].Total <= 0 {
		t.Errorf("summary totals = %+v, want a positive goroutine count first", g.Totals)
	}
}

func TestRunFiles_lenientAllUnusable(t *testing.T) {
	t.Parallel()

	garbage := writeProfileFile(t, "garbage.pb.gz", []byte("junk"))
	_, err := RunFiles(FileOptions{Paths: []string{garbage}, Lenient: true})
	if err == nil || !strings.Contains(err.Error(), "no analyzable profiles") {
		t.Errorf("err = %v, want 'no analyzable profiles'", err)
	}
}

func TestClassifyProfile_contentionByFilename(t *testing.T) {
	t.Parallel()

	src := realFixture(t, "block.pb.gz")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read block fixture: %v", err)
	}
	prof, err := profanalyze.ParseBytes(src, raw)
	if err != nil {
		t.Fatalf("parse block fixture: %v", err)
	}

	tests := []struct {
		path    string
		want    profile.Type
		wantErr bool
	}{
		{path: "captures/block.pb.gz", want: profile.TypeBlock},
		{path: "captures/mutex.pb.gz", want: profile.TypeMutex},
		{path: "captures/renamed.pb.gz", wantErr: true},
	}
	for _, tt := range tests {
		got, detectable, err := classifyProfile(prof, tt.path)
		if tt.wantErr {
			if err == nil || !strings.Contains(err.Error(), "block or mutex") {
				t.Errorf("classify(%s): err = %v, want block-or-mutex hint", tt.path, err)
			}
			continue
		}
		if err != nil || detectable || got != tt.want {
			t.Errorf("classify(%s) = (%q, %v, %v), want (%q, false, nil)", tt.path, got, detectable, err, tt.want)
		}
	}
}

func TestWriteText_fileReportWithTop(t *testing.T) {
	t.Parallel()

	path := writeProfileFile(t, "cpu.pb.gz", marshalProfile(t, heavyJSONCPUProfile()))
	rep, err := RunFiles(FileOptions{Paths: []string{path}, TopN: 2})
	if err != nil {
		t.Fatalf("RunFiles: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteText(&buf, rep); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"gopher-sage report — saved profiles",
		"cpu [" + path + "] profile",
		"top 2 functions",
		"function",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
