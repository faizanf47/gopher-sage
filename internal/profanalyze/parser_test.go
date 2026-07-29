package profanalyze

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pp "github.com/google/pprof/profile"
)

// writeProfile serialises p to a temp .pb.gz file the package's
// Load() can read. Returns the file path.
func writeProfile(t *testing.T, p *pp.Profile) string {
	t.Helper()
	if err := p.CheckValid(); err != nil {
		t.Fatalf("CheckValid: %v", err)
	}
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	path := filepath.Join(t.TempDir(), "fixture.pb.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

// cpuProfile builds a minimal CPU profile with one sample at one
// function. The "cpu" sample type matches what a captured
// /debug/pprof/profile dump carries.
func cpuProfile(t *testing.T) *pp.Profile {
	t.Helper()
	fn := &pp.Function{ID: 1, Name: "main.dummy", SystemName: "main.dummy", Filename: "dummy.go"}
	loc := &pp.Location{ID: 1, Line: []pp.Line{{Function: fn, Line: 1}}}
	return &pp.Profile{
		SampleType: []*pp.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &pp.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10_000_000,
		Function:   []*pp.Function{fn},
		Location:   []*pp.Location{loc},
		Sample: []*pp.Sample{
			{Location: []*pp.Location{loc}, Value: []int64{100}},
		},
	}
}

func TestLoad_validProfile(t *testing.T) {
	t.Parallel()
	path := writeProfile(t, cpuProfile(t))
	prof, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if prof.Raw == nil {
		t.Fatal("Load returned nil Raw")
	}
	if prof.Path != path {
		t.Errorf("Path = %q, want %q", prof.Path, path)
	}
}

func TestLoad_missingFile(t *testing.T) {
	t.Parallel()
	if _, err := Load(""); err == nil {
		t.Fatal("expected error for empty path")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "nope.pb.gz")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_invalidProfile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "garbage.pb.gz")
	if err := os.WriteFile(path, []byte("not a profile"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid profile")
	}
}

func TestResolveSampleIndex(t *testing.T) {
	t.Parallel()

	// A heap-shaped profile with all four sample types.
	heap := &pp.Profile{
		SampleType: []*pp.ValueType{
			{Type: "alloc_objects", Unit: "count"},
			{Type: "alloc_space", Unit: "bytes"},
			{Type: "inuse_objects", Unit: "count"},
			{Type: "inuse_space", Unit: "bytes"},
		},
		PeriodType:        &pp.ValueType{Type: "space", Unit: "bytes"},
		Period:            524288,
		DefaultSampleType: "inuse_space",
	}
	heapProf := &Profile{Raw: heap}

	cpu := cpuProfile(t)
	cpuProf := &Profile{Raw: cpu}

	tests := []struct {
		name     string
		prof     *Profile
		input    SampleIndex
		wantIdx  int
		wantName SampleIndex
		wantErr  bool
	}{
		{name: "heap default → inuse_space", prof: heapProf, input: "", wantIdx: 3, wantName: SampleInuseSpace},
		{name: "heap explicit alloc_space", prof: heapProf, input: SampleAllocSpace, wantIdx: 1, wantName: SampleAllocSpace},
		{name: "heap inuse_objects", prof: heapProf, input: SampleInuseObjects, wantIdx: 2, wantName: SampleInuseObjects},
		{name: "heap unknown index", prof: heapProf, input: "garbage", wantErr: true},

		{name: "cpu default → first column", prof: cpuProf, input: "", wantIdx: 0, wantName: SampleCPU},
		{name: "cpu alias 'cpu' resolves", prof: cpuProf, input: SampleCPU, wantIdx: 0, wantName: SampleCPU},
		{name: "cpu alias 'samples' falls through to 'cpu'", prof: cpuProf, input: SampleSamples, wantIdx: 0, wantName: SampleCPU},
		{name: "cpu profile has no inuse_space", prof: cpuProf, input: SampleInuseSpace, wantErr: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			idx, name, _, err := tc.prof.ResolveSampleIndex(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got idx=%d name=%q", idx, name)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if idx != tc.wantIdx {
				t.Errorf("idx = %d, want %d", idx, tc.wantIdx)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
		})
	}
}

func TestAvailableSampleTypes(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: &pp.Profile{SampleType: []*pp.ValueType{
		{Type: "cpu", Unit: "nanoseconds"},
	}}}
	got := strings.Join(prof.AvailableSampleTypes(), ",")
	if got != "cpu" {
		t.Errorf("AvailableSampleTypes = %q, want %q", got, "cpu")
	}
}
