package profanalyze

import (
	"regexp"
	"strings"
	"testing"
)

// fakeDetector implements Detector with arbitrary metadata for
// registry validation tests.
type fakeDetector struct{ meta Metadata }

func (f fakeDetector) Meta() Metadata             { return f.meta }
func (f fakeDetector) Detect(DetectCtx) []Finding { return nil }

func validMeta() Metadata {
	return Metadata{
		Scope:       ScopeCPU,
		Num:         99,
		Name:        "fake-detector",
		Checks:      "something",
		Method:      "somehow",
		Limitations: "some blind spot",
	}
}

func TestMetadata_ID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta Metadata
		want string
	}{
		{"cpu", Metadata{Scope: ScopeCPU, Num: 1}, "CPU-001"},
		{"heap", Metadata{Scope: ScopeHeap, Num: 7}, "HEAP-007"},
		{"three digits", Metadata{Scope: ScopeCPU, Num: 123}, "CPU-123"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.meta.ID(); got != tc.want {
				t.Errorf("ID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRegistry_rejectsInvalidRegistrations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Metadata)
		wantErr string
	}{
		{"unknown scope", func(m *Metadata) { m.Scope = "goroutine" }, "unknown scope"},
		{"zero num", func(m *Metadata) { m.Num = 0 }, "must be positive"},
		{"negative num", func(m *Metadata) { m.Num = -3 }, "must be positive"},
		{"missing name", func(m *Metadata) { m.Name = "" }, "name is required"},
		{"missing checks", func(m *Metadata) { m.Checks = "" }, "must declare what it checks"},
		{"missing method", func(m *Metadata) { m.Method = "" }, "must declare how it works"},
		{"missing limitations", func(m *Metadata) { m.Limitations = "" }, "must declare its limitations"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta := validMeta()
			tc.mutate(&meta)
			err := NewRegistry().Register(fakeDetector{meta: meta})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Register = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}

	if err := NewRegistry().Register(nil); err == nil {
		t.Error("Register(nil): expected error")
	}
}

func TestRegistry_rejectsDuplicates(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	if err := r.Register(fakeDetector{meta: validMeta()}); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	if err := r.Register(fakeDetector{meta: validMeta()}); err == nil ||
		!strings.Contains(err.Error(), "already taken") {
		t.Errorf("duplicate ID: Register = %v, want 'already taken'", err)
	}

	sameNameNewID := validMeta()
	sameNameNewID.Num = 100
	if err := r.Register(fakeDetector{meta: sameNameNewID}); err == nil ||
		!strings.Contains(err.Error(), "already registered") {
		t.Errorf("duplicate name: Register = %v, want 'already registered'", err)
	}
}

func TestRegistry_detectorsOrderedByID(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	// Register out of order; Detectors() must come back sorted.
	for _, meta := range []Metadata{
		{Scope: ScopeHeap, Num: 2, Name: "h2", Checks: "c", Method: "m", Limitations: "l"},
		{Scope: ScopeCPU, Num: 5, Name: "c5", Checks: "c", Method: "m", Limitations: "l"},
		{Scope: ScopeCPU, Num: 1, Name: "c1", Checks: "c", Method: "m", Limitations: "l"},
		{Scope: ScopeHeap, Num: 1, Name: "h1", Checks: "c", Method: "m", Limitations: "l"},
	} {
		if err := r.Register(fakeDetector{meta: meta}); err != nil {
			t.Fatalf("Register %s: %v", meta.Name, err)
		}
	}

	var got []string
	for _, d := range r.Detectors() {
		got = append(got, d.Meta().ID())
	}
	want := []string{"CPU-001", "CPU-005", "HEAP-001", "HEAP-002"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// TestDefaultRegistry_transparencyContract walks every built-in
// detector and asserts the registration contract held: valid static
// ID, and non-empty Checks / Method / Limitations. A detector that
// cannot explain itself does not belong in the default set.
func TestDefaultRegistry_transparencyContract(t *testing.T) {
	t.Parallel()

	cat := Catalog()
	if len(cat) != 14 {
		t.Fatalf("default registry has %d detectors, want 14", len(cat))
	}

	idPattern := regexp.MustCompile(`^(CPU|HEAP)-\d{3}$`)
	seenIDs := make(map[string]bool)
	seenNames := make(map[string]bool)
	for _, m := range cat {
		id := m.ID()
		if !idPattern.MatchString(id) {
			t.Errorf("%s: ID %q does not match %s", m.Name, id, idPattern)
		}
		if seenIDs[id] {
			t.Errorf("duplicate ID %s", id)
		}
		if seenNames[m.Name] {
			t.Errorf("duplicate name %s", m.Name)
		}
		seenIDs[id] = true
		seenNames[m.Name] = true

		if m.Checks == "" || m.Method == "" || m.Limitations == "" {
			t.Errorf("%s (%s): transparency fields must be non-empty", m.Name, id)
		}
	}
}

// TestDefaultRegistry_findingsCarryDetectorID ensures a finding can
// be traced back to the registered detector that produced it.
func TestDefaultRegistry_findingsCarryDetectorID(t *testing.T) {
	t.Parallel()

	findings, err := Run(&Profile{Raw: heavyJSONCPUProfile()}, DefaultDetectors())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	for _, f := range findings {
		if f.ID == "" {
			t.Errorf("finding %q has no detector ID", f.Detector)
		}
	}
	got, ok := findingsByName(findings)["high-json-cpu"]
	if !ok {
		t.Fatal("expected high-json-cpu finding")
	}
	if got.ID != "CPU-001" {
		t.Errorf("high-json-cpu ID = %q, want CPU-001", got.ID)
	}
}
