package profanalyze

import (
	"strings"
	"testing"
)

// These tests pin detector behavior against real captured profiles
// from fixtures/sources/leaky_server (fixtures/profiles/*.pb.gz).
// Assertions are presence-only so a re-capture with different load
// timing still passes; exact shares belong to the unit tests over
// synthetic profiles.

func TestFixtureRegression_cpuProfile(t *testing.T) {
	t.Parallel()

	prof := loadFixtureProfile(t, "cpu.pb.gz")
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	by := findingsByName(findings)

	// The leaky server compiles its regexp once per loop iteration —
	// the detector must catch it, name the compile frames (including
	// the regexp/syntax machinery), upgrade the recommendation, and
	// attribute the cost to the offending function.
	re, ok := by["high-regexp-cpu"]
	if !ok {
		t.Fatalf("high-regexp-cpu did not fire on the leaky-server CPU fixture; findings: %v", detectorNames(findings))
	}
	if !containsAny(re.Functions, "regexp.Compile", "regexp.MustCompile") {
		t.Errorf("regexp functions missing compile frames: %v", re.Functions)
	}
	if !containsSubstring(re.Functions, "regexp/syntax.") {
		t.Errorf("regexp functions missing regexp/syntax frames: %v", re.Functions)
	}
	if !strings.Contains(re.Recommendation, "per call") {
		t.Errorf("recommendation not upgraded to per-call compilation: %q", re.Recommendation)
	}
	if !callSitesContain(re.CallSites, "main.processItems") {
		t.Errorf("regexp call sites missing main.processItems: %+v", re.CallSites)
	}

	// The allocation-heavy handler shows up as GC pressure with the
	// cost attributed to user code, not just runtime frames.
	gc, ok := by["high-gc-cpu"]
	if !ok {
		t.Fatalf("high-gc-cpu did not fire on the leaky-server CPU fixture; findings: %v", detectorNames(findings))
	}
	if !callSitesContain(gc.CallSites, "main.") {
		t.Errorf("gc call sites name no main.* function: %+v", gc.CallSites)
	}

	assertFindingsCarryUnits(t, findings)
}

func TestFixtureRegression_heapProfile(t *testing.T) {
	t.Parallel()

	prof := loadFixtureProfile(t, "heap.pb.gz")
	findings, err := Run(prof, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	by := findingsByName(findings)

	// buildPayload appends 64 KiB per request with no pre-allocation
	// and the result is cached forever — it must lead both the churn
	// and the retention rankings.
	for _, name := range []string{"high-alloc-space", "high-inuse-space"} {
		f, ok := by[name]
		if !ok {
			t.Fatalf("%s did not fire on the leaky-server heap fixture; findings: %v", name, detectorNames(findings))
		}
		if !containsSubstring(f.Functions, "main.buildPayload") {
			t.Errorf("%s functions missing main.buildPayload: %v", name, f.Functions)
		}
	}

	// Native heap profiles strip leading runtime frames, so the
	// all-runtime categories must stay silent despite the server's
	// heavy map writes and string concatenation.
	for _, name := range []string{"string-concat-allocation", "map-growth-pressure"} {
		if f, fired := by[name]; fired {
			t.Errorf("%s fired on a native heap profile: %+v", name, f)
		}
	}

	assertFindingsCarryUnits(t, findings)
}

// assertFindingsCarryUnits checks the unit-evidence contract on real
// data: every finding names its unit and a positive absolute value.
func assertFindingsCarryUnits(t *testing.T, findings []Finding) {
	t.Helper()
	for _, f := range findings {
		if f.Unit == "" {
			t.Errorf("%s: empty Unit", f.ID)
		}
		if f.MatchedValue <= 0 {
			t.Errorf("%s: MatchedValue = %d, want > 0", f.ID, f.MatchedValue)
		}
	}
}
