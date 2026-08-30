package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/faizanf47/gopher-sage/internal/analyze"
)

func TestAgent_helpShowsWorkflow(t *testing.T) {
	t.Parallel()
	stdout, err := execCLI(t, "agent")
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	for _, want := range []string{"optimize loop", "call sites", "capture", "report", "detector ID"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("agent help missing %q:\n%s", want, stdout)
		}
	}
}

func TestAgent_flagValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"capture missing flags", []string{"agent", "capture"}, `"out", "server" not set`},
		{"report no source", []string{"agent", "report"}, "at least one of the flags"},
		{"report both sources", []string{"agent", "report", "--dir", "d", "--file", "f"}, "none of the others can be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := execCLI(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("err = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

// TestAgent_captureThenReport walks the loop's mechanical half
// against a stub pprof endpoint serving the captured fixtures:
// capture writes the files and prints the chain hint, report reads
// them back and produces findings with call sites.
func TestAgent_captureThenReport(t *testing.T) {
	t.Parallel()

	cpuRaw, err := os.ReadFile(fixturePath(t, "cpu.pb.gz"))
	if err != nil {
		t.Fatalf("read cpu fixture: %v", err)
	}
	heapRaw, err := os.ReadFile(fixturePath(t, "heap.pb.gz"))
	if err != nil {
		t.Fatalf("read heap fixture: %v", err)
	}
	goroutineRaw, err := os.ReadFile(fixturePath(t, "goroutine.pb.gz"))
	if err != nil {
		t.Fatalf("read goroutine fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/debug/pprof/profile":
			_, _ = w.Write(cpuRaw)
		case "/debug/pprof/heap":
			_, _ = w.Write(heapRaw)
		case "/debug/pprof/goroutine":
			_, _ = w.Write(goroutineRaw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// No --types: the default must fetch cpu, heap AND goroutine.
	dir := filepath.Join(t.TempDir(), "before")
	stdout, err := execCLI(t, "agent", "capture", "--server", srv.URL, "--seconds", "1", "-o", dir)
	if err != nil {
		t.Fatalf("agent capture: %v", err)
	}
	for _, want := range []string{
		"wrote " + filepath.Join(dir, "cpu.pb.gz"),
		"wrote " + filepath.Join(dir, "heap.pb.gz"),
		"wrote " + filepath.Join(dir, "goroutine.pb.gz"),
		"next: gopher-sage agent report --dir " + dir,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("capture output missing %q:\n%s", want, stdout)
		}
	}

	stdout, err = execCLI(t, "agent", "report", "--dir", dir)
	if err != nil {
		t.Fatalf("agent report: %v", err)
	}
	for _, want := range []string{"CPU-", "HEAP-", "call sites:", "suggestion:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report output missing %q:\n%s", want, stdout)
		}
	}

	stdout, err = execCLI(t, "agent", "report", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("agent report --json: %v", err)
	}
	var rep analyze.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("report JSON does not decode into analyze.Report: %v", err)
	}
	if len(rep.Profiles) != 3 {
		t.Errorf("JSON report has %d profiles, want 3 (cpu, heap, goroutine summary)", len(rep.Profiles))
	}
}

// TestAgent_reportMixedDir is the regression test for the field-test
// trap: a goroutine profile sitting next to cpu/heap captures used to
// abort the entire report with zero findings emitted.
func TestAgent_reportMixedDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"cpu.pb.gz", "heap.pb.gz", "goroutine.pb.gz"} {
		raw, err := os.ReadFile(fixturePath(t, name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	stdout, err := execCLI(t, "agent", "report", "--dir", dir)
	if err != nil {
		t.Fatalf("agent report on mixed dir: %v", err)
	}
	for _, want := range []string{
		"CPU-", "HEAP-", // detector findings still present
		"goroutine [", // goroutine summary section
		"totals: goroutine ",
		"no detectors cover this profile type",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("mixed-dir report missing %q:\n%s", want, stdout)
		}
	}

	stdout, err = execCLI(t, "agent", "report", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("agent report --json: %v", err)
	}
	var rep analyze.Report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("mixed-dir JSON does not decode: %v", err)
	}
	if len(rep.Profiles) != 3 {
		t.Errorf("JSON report has %d profiles, want 3", len(rep.Profiles))
	}
}

func TestAgentCapture_json(t *testing.T) {
	t.Parallel()

	heapRaw, err := os.ReadFile(fixturePath(t, "heap.pb.gz"))
	if err != nil {
		t.Fatalf("read heap fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(heapRaw)
	}))
	defer srv.Close()

	dir := t.TempDir()
	stdout, err := execCLI(t, "agent", "capture", "--server", srv.URL, "--types", "heap", "-o", dir, "--json")
	if err != nil {
		t.Fatalf("agent capture --json: %v", err)
	}
	var got struct {
		Server string                 `json:"server"`
		Files  []analyze.CapturedFile `json:"files"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("capture JSON does not decode: %v", err)
	}
	if got.Server != srv.URL || len(got.Files) != 1 || got.Files[0].Bytes != len(heapRaw) {
		t.Errorf("capture JSON = %+v, want server %s and one %d-byte file", got, srv.URL, len(heapRaw))
	}
}

func TestResolveProfileArgs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"heap.pb.gz", "cpu.pb.gz", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	empty := t.TempDir()

	tests := []struct {
		name    string
		dir     string
		files   string
		want    []string
		wantSub string
	}{
		{
			name: "dir expands to sorted pb.gz",
			dir:  dir,
			want: []string{filepath.Join(dir, "cpu.pb.gz"), filepath.Join(dir, "heap.pb.gz")},
		},
		{
			name:  "file list passes through",
			files: " a.pb.gz , b.pb.gz ",
			want:  []string{"a.pb.gz", "b.pb.gz"},
		},
		{
			name:    "empty dir",
			dir:     empty,
			wantSub: "no .pb.gz profiles",
		},
		{
			name:    "missing dir",
			dir:     filepath.Join(empty, "absent"),
			wantSub: "read profile directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveProfileArgs(tt.dir, tt.files)
			if tt.wantSub != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
					t.Errorf("err = %v, want substring %q", err, tt.wantSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveProfileArgs: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
