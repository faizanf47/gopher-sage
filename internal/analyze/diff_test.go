package analyze

import (
	"bytes"
	"strings"
	"testing"

	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// mkFinding builds the minimal finding a diff needs: identity plus
// the two numbers the label algorithm reads.
func mkFinding(id, sampleType, unit string, share float64, value int64) profanalyze.Finding {
	return profanalyze.Finding{
		ID:           id,
		Detector:     "det-" + id,
		Title:        "title " + id,
		SampleType:   sampleType,
		Unit:         unit,
		SharePerc:    share,
		MatchedValue: value,
	}
}

func cpuReport(findings ...profanalyze.Finding) Report {
	return Report{Profiles: []ProfileReport{{
		Type: profile.TypeCPU, DurationNanos: 20_000_000_000, Findings: findings,
	}}}
}

func heapReport(durationNanos int64, findings ...profanalyze.Finding) Report {
	return Report{Profiles: []ProfileReport{{
		Type: profile.TypeHeap, DurationNanos: durationNanos, Findings: findings,
	}}}
}

func TestDiff_labels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		before    Report
		after     Report
		wantLabel DiffLabel
		wantNote  string // substring; empty = note must be empty
	}{
		{
			name:      "fixed when detector disappears",
			before:    cpuReport(mkFinding("CPU-002", "cpu", "nanoseconds", 31, 2_200_000_000)),
			after:     cpuReport(),
			wantLabel: LabelFixed,
		},
		{
			name:      "new cpu finding carries no note",
			before:    cpuReport(),
			after:     cpuReport(mkFinding("CPU-001", "cpu", "nanoseconds", 8, 100_000_000)),
			wantLabel: LabelNew,
		},
		{
			name:      "new alloc finding gets the uptime note",
			before:    heapReport(0),
			after:     heapReport(0, mkFinding("HEAP-001", "alloc_space", "bytes", 8, 1<<20)),
			wantLabel: LabelNew,
			wantNote:  "alloc_* accumulates",
		},
		{
			name:      "improved when both axes fall",
			before:    cpuReport(mkFinding("CPU-002", "cpu", "nanoseconds", 31, 2_200_000_000)),
			after:     cpuReport(mkFinding("CPU-002", "cpu", "nanoseconds", 10.34, 150_000_000)),
			wantLabel: LabelImproved,
		},
		{
			name:      "worse when both axes rise on cpu",
			before:    cpuReport(mkFinding("CPU-004", "cpu", "nanoseconds", 8.1, 100_000_000)),
			after:     cpuReport(mkFinding("CPU-004", "cpu", "nanoseconds", 14.3, 200_000_000)),
			wantLabel: LabelWorse,
		},
		{
			name:      "alloc rise is inconclusive, never worse",
			before:    heapReport(60_000_000_000, mkFinding("HEAP-001", "alloc_space", "bytes", 86.1, 348<<20)),
			after:     heapReport(300_000_000_000, mkFinding("HEAP-001", "alloc_space", "bytes", 91.0, 442<<20)),
			wantLabel: LabelInconclusive,
			wantNote:  "Process uptime at capture",
		},
		{
			name:      "share redistribution alone stays unchanged",
			before:    cpuReport(mkFinding("CPU-005", "cpu", "nanoseconds", 12, 151_000_000)),
			after:     cpuReport(mkFinding("CPU-005", "cpu", "nanoseconds", 7, 181_000_000)), // share -5, value +20%
			wantLabel: LabelUnchanged,
		},
		{
			name:      "load scaling alone stays unchanged",
			before:    cpuReport(mkFinding("CPU-006", "cpu", "nanoseconds", 4.1, 50_000_000)),
			after:     cpuReport(mkFinding("CPU-006", "cpu", "nanoseconds", 4.4, 150_000_000)), // value +200%, share +0.3
			wantLabel: LabelUnchanged,
		},
		{
			name:      "sub-threshold movement stays unchanged",
			before:    cpuReport(mkFinding("CPU-001", "cpu", "nanoseconds", 10, 100_000_000)),
			after:     cpuReport(mkFinding("CPU-001", "cpu", "nanoseconds", 8.1, 91_000_000)), // -1.9 pts, -9%
			wantLabel: LabelUnchanged,
		},
		{
			name:      "exact thresholds count as movement",
			before:    cpuReport(mkFinding("CPU-001", "cpu", "nanoseconds", 12, 100_000_000)),
			after:     cpuReport(mkFinding("CPU-001", "cpu", "nanoseconds", 10, 90_000_000)), // -2.0 pts, -10%
			wantLabel: LabelImproved,
		},
		{
			name:      "zero before value falls back to share-only",
			before:    cpuReport(mkFinding("CPU-003", "cpu", "nanoseconds", 3, 0)),
			after:     cpuReport(mkFinding("CPU-003", "cpu", "nanoseconds", 8, 90_000_000)),
			wantLabel: LabelWorse,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := Diff(tt.before, tt.after, "b", "a")
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if len(d.Profiles) != 1 || len(d.Profiles[0].Findings) != 1 {
				t.Fatalf("want exactly one finding diff, got %+v", d.Profiles)
			}
			fd := d.Profiles[0].Findings[0]
			if fd.Label != tt.wantLabel {
				t.Errorf("label = %q, want %q (delta %+.2f pts / %+.2f%%)", fd.Label, tt.wantLabel, fd.ShareDeltaPts, fd.ValueDeltaPerc)
			}
			if tt.wantNote == "" && fd.Note != "" {
				t.Errorf("unexpected note %q", fd.Note)
			}
			if tt.wantNote != "" && !strings.Contains(fd.Note, tt.wantNote) {
				t.Errorf("note %q missing %q", fd.Note, tt.wantNote)
			}
		})
	}
}

func TestDiff_cpuWindowMismatch(t *testing.T) {
	t.Parallel()

	mk := func(window int64) Report {
		return Report{Profiles: []ProfileReport{{Type: profile.TypeCPU, DurationNanos: window, Findings: []profanalyze.Finding{}}}}
	}

	d, err := Diff(mk(20_000_000_000), mk(30_000_000_000), "b", "a")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !d.Profiles[0].DurationMismatch {
		t.Error("20s vs 30s: DurationMismatch not set")
	}
	if len(d.Warnings) == 0 || !strings.Contains(d.Warnings[0], "not comparable") {
		t.Errorf("warnings = %v, want a comparability warning", d.Warnings)
	}

	d, err = Diff(mk(20_000_000_000), mk(21_000_000_000), "b", "a")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if d.Profiles[0].DurationMismatch || len(d.Warnings) != 0 {
		t.Errorf("20s vs 21s should be within tolerance: mismatch=%v warnings=%v", d.Profiles[0].DurationMismatch, d.Warnings)
	}
}

func TestDiff_structural(t *testing.T) {
	t.Parallel()

	t.Run("one-sided profile", func(t *testing.T) {
		t.Parallel()
		d, err := Diff(heapReport(0), cpuReport(), "b", "a")
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		if len(d.Profiles) != 2 || !d.Profiles[0].BeforeOnly || !d.Profiles[1].AfterOnly {
			t.Errorf("profiles = %+v, want heap before-only then cpu after-only", d.Profiles)
		}
		if len(d.Warnings) != 2 {
			t.Errorf("warnings = %v, want one per one-sided profile", d.Warnings)
		}
	})

	t.Run("duplicate profile type errors", func(t *testing.T) {
		t.Parallel()
		dup := Report{Profiles: []ProfileReport{{Type: profile.TypeCPU}, {Type: profile.TypeCPU}}}
		if _, err := Diff(dup, cpuReport(), "b", "a"); err == nil || !strings.Contains(err.Error(), "more than one") {
			t.Errorf("err = %v, want duplicate-profile error", err)
		}
	})

	t.Run("duplicate finding ID keeps first and warns", func(t *testing.T) {
		t.Parallel()
		before := cpuReport(
			mkFinding("CPU-001", "cpu", "nanoseconds", 10, 100),
			mkFinding("CPU-001", "cpu", "nanoseconds", 99, 999),
		)
		d, err := Diff(before, cpuReport(mkFinding("CPU-001", "cpu", "nanoseconds", 10, 100)), "b", "a")
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		if len(d.Profiles[0].Findings) != 1 || d.Profiles[0].Findings[0].BeforeShare != 10 {
			t.Errorf("findings = %+v, want the first CPU-001 kept", d.Profiles[0].Findings)
		}
		if len(d.Warnings) == 0 || !strings.Contains(d.Warnings[0], "duplicate finding") {
			t.Errorf("warnings = %v, want duplicate-finding warning", d.Warnings)
		}
	})
}

func TestDiff_goroutineSummary(t *testing.T) {
	t.Parallel()

	mk := func(total int64, entries ...profanalyze.TopEntry) Report {
		return Report{Profiles: []ProfileReport{{
			Type:     profile.TypeGoroutine,
			Summary:  true,
			Totals:   []profanalyze.SampleTotal{{SampleType: "goroutine", Unit: "count", Total: total}},
			Top:      &profanalyze.TopReport{SampleType: "goroutine", Entries: entries},
			Findings: []profanalyze.Finding{},
		}}}
	}
	before := mk(141053,
		profanalyze.TopEntry{Function: "main.leakyWorker", Cum: 96250},
		profanalyze.TopEntry{Function: "main.emitMetric", Cum: 44797},
	)
	after := mk(4, profanalyze.TopEntry{Function: "runtime.gopark", Cum: 4})

	d, err := Diff(before, after, "b", "a")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	pd := d.Profiles[0]
	if pd.Totals[0].Before != 141053 || pd.Totals[0].After != 4 {
		t.Errorf("goroutine totals = %+v, want 141053 → 4", pd.Totals[0])
	}
	byFn := map[string]TopDelta{}
	for _, td := range pd.TopDeltas {
		byFn[td.Function] = td
	}
	if td := byFn["main.leakyWorker"]; td.Before != 96250 || td.After != 0 {
		t.Errorf("leakyWorker delta = %+v, want 96250 → 0", td)
	}
	if td := byFn["runtime.gopark"]; td.Before != 0 || td.After != 4 {
		t.Errorf("gopark delta = %+v, want 0 → 4", td)
	}
}

// TestDiff_selfIsUnchanged pins the identity property over the real
// fixture set: a capture diffed against itself moves nothing.
func TestDiff_selfIsUnchanged(t *testing.T) {
	t.Parallel()

	paths := []string{
		realFixture(t, "cpu.pb.gz"),
		realFixture(t, "heap.pb.gz"),
		realFixture(t, "goroutine.pb.gz"),
	}
	rep, err := RunFiles(FileOptions{Paths: paths, TopN: 10, Lenient: true})
	if err != nil {
		t.Fatalf("RunFiles: %v", err)
	}
	d, err := Diff(rep, rep, "x", "x")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(d.Warnings) != 0 {
		t.Errorf("self-diff warnings: %v", d.Warnings)
	}
	for _, pd := range d.Profiles {
		for _, fd := range pd.Findings {
			if fd.Label != LabelUnchanged {
				t.Errorf("%s %s: label %q on self-diff, want unchanged", pd.Type, fd.ID, fd.Label)
			}
		}
		for _, td := range pd.Totals {
			if td.DeltaPerc != 0 || td.Before != td.After {
				t.Errorf("%s total %s moved on self-diff: %+v", pd.Type, td.SampleType, td)
			}
		}
	}
}

func TestWriteDiffText(t *testing.T) {
	t.Parallel()

	before := Report{Profiles: []ProfileReport{
		{
			Type: profile.TypeCPU, DurationNanos: 20_000_000_000,
			Totals: []profanalyze.SampleTotal{{SampleType: "cpu", Unit: "nanoseconds", Total: 7_000_000_000}},
			Findings: []profanalyze.Finding{
				mkFinding("CPU-002", "cpu", "nanoseconds", 31, 2_200_000_000),
				mkFinding("CPU-004", "cpu", "nanoseconds", 5.1, 350_000_000),
			},
		},
		heapReport(60_000_000_000, mkFinding("HEAP-001", "alloc_space", "bytes", 86.1, 348<<20)).Profiles[0],
	}}
	after := Report{Profiles: []ProfileReport{
		{
			Type: profile.TypeCPU, DurationNanos: 20_000_000_000,
			Totals: []profanalyze.SampleTotal{{SampleType: "cpu", Unit: "nanoseconds", Total: 1_300_000_000}},
			Findings: []profanalyze.Finding{
				mkFinding("CPU-002", "cpu", "nanoseconds", 10.34, 150_000_000),
			},
		},
		heapReport(300_000_000_000, mkFinding("HEAP-001", "alloc_space", "bytes", 91.0, 442<<20)).Profiles[0],
	}}

	d, err := Diff(before, after, ".gs/before", ".gs/after")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	var buf bytes.Buffer
	if err := analyzeWriteDiff(&buf, d); err != nil {
		t.Fatalf("WriteDiffText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"gopher-sage diff — .gs/before → .gs/after",
		"cpu profile (window 20.0s → 20.0s)",
		"totals: cpu 7.0s → 1.3s (-81%)",
		"[improved    ] CPU-002",
		"[fixed       ] CPU-004",
		"→ not detected",
		"[inconclusive] HEAP-001",
		"note: alloc_* accumulates",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff text missing %q:\n%s", want, out)
		}
	}
}

// analyzeWriteDiff exists so the test reads as a call through the
// public surface.
func analyzeWriteDiff(buf *bytes.Buffer, d DiffReport) error { return WriteDiffText(buf, d) }
