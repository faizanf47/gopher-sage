package profanalyze

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Metadata is the transparency contract every detector publishes.
// It is what makes the detector set auditable: a user can read the
// catalog (`gopher-sage -detectors`) and know exactly what is being
// checked, how the decision is made, and where the blind spots are —
// without reading the source.
//
// Scope+Num form the detector's static ID (see Metadata.ID). The
// number is assigned once when the detector is first added and must
// never be renumbered or reused: IDs appear in reports and may be
// referenced by user tooling long after a detector is retired.
type Metadata struct {
	// Scope is the kind of profile the detector applies to.
	Scope Scope `json:"scope"`
	// Num is the detector's static number within its scope.
	Num int `json:"num"`
	// Name is the stable kebab-case slug carried on findings.
	Name string `json:"name"`
	// Checks says what the detector looks for, in one sentence.
	Checks string `json:"checks"`
	// Method explains how the detector decides — the data it reads,
	// the frames it matches, and the thresholds it applies.
	Method string `json:"method"`
	// Limitations states what the detector cannot know: its
	// false-positive and false-negative surface.
	Limitations string `json:"limitations"`
}

// ID returns the detector's static identifier, composed from scope
// and number: "CPU-001", "HEAP-007". It is stable across runs,
// builds, and file renames.
func (m Metadata) ID() string {
	return fmt.Sprintf("%s-%03d", strings.ToUpper(string(m.Scope)), m.Num)
}

// validate enforces the registration contract. Transparency fields
// are mandatory: a detector that cannot say what it checks, how it
// works, and where it is blind does not get registered.
func (m Metadata) validate() error {
	switch m.Scope {
	case ScopeCPU, ScopeHeap:
	default:
		return fmt.Errorf("unknown scope %q", m.Scope)
	}
	if m.Num <= 0 {
		return fmt.Errorf("static number must be positive, got %d", m.Num)
	}
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if m.Checks == "" {
		return fmt.Errorf("detector %q must declare what it checks", m.Name)
	}
	if m.Method == "" {
		return fmt.Errorf("detector %q must declare how it works", m.Name)
	}
	if m.Limitations == "" {
		return fmt.Errorf("detector %q must declare its limitations", m.Name)
	}
	return nil
}

// Registry is the central collection of detectors. Detectors
// register themselves (each detector file carries an init() that
// calls MustRegister); consumers read the set back with Detectors()
// in a deterministic order.
type Registry struct {
	mu     sync.Mutex
	byID   map[string]Detector
	byName map[string]string // name → ID, for duplicate-name errors
}

// NewRegistry returns an empty registry. Most code uses the package
// default registry via MustRegister / DefaultDetectors; independent
// registries exist for tests and embedders that want a custom set.
func NewRegistry() *Registry {
	return &Registry{
		byID:   make(map[string]Detector),
		byName: make(map[string]string),
	}
}

// Register adds d to the registry after validating its metadata and
// the uniqueness of its static ID and name.
func (r *Registry) Register(d Detector) error {
	if d == nil {
		return fmt.Errorf("profanalyze: register: detector is nil")
	}
	meta := d.Meta()
	if err := meta.validate(); err != nil {
		return fmt.Errorf("profanalyze: register: %w", err)
	}
	id := meta.ID()

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byID[id]; exists {
		return fmt.Errorf("profanalyze: register %q: ID %s is already taken", meta.Name, id)
	}
	if takenBy, exists := r.byName[meta.Name]; exists {
		return fmt.Errorf("profanalyze: register: name %q is already registered as %s", meta.Name, takenBy)
	}
	r.byID[id] = d
	r.byName[meta.Name] = id
	return nil
}

// MustRegister is Register for init()-time use: registration of a
// compile-time-known detector can only fail on a programming error,
// so it panics instead of returning one.
func (r *Registry) MustRegister(d Detector) {
	if err := r.Register(d); err != nil {
		panic(err)
	}
}

// Detectors returns the registered detectors ordered by static ID
// (scope, then number), so detector iteration — and therefore
// finding order — is deterministic regardless of registration order.
func (r *Registry) Detectors() []Detector {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Detector, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.byID[id])
	}
	return out
}

// Catalog returns the metadata of every registered detector in
// Detectors() order. This is the transparency surface: everything a
// detector advertises about itself, without running anything.
func (r *Registry) Catalog() []Metadata {
	dets := r.Detectors()
	out := make([]Metadata, 0, len(dets))
	for _, d := range dets {
		out = append(out, d.Meta())
	}
	return out
}

// defaultRegistry holds the built-in detector set. Each detector
// file registers into it from init().
var defaultRegistry = NewRegistry()

// MustRegister registers d in the default registry. Detector files
// call this from init(); it panics on a malformed or conflicting
// registration so a bad detector fails fast at startup.
func MustRegister(d Detector) {
	defaultRegistry.MustRegister(d)
}

// DefaultDetectors returns every detector registered in the default
// registry, ordered by static ID.
func DefaultDetectors() []Detector {
	return defaultRegistry.Detectors()
}

// Catalog returns the metadata of every detector in the default
// registry, ordered by static ID.
func Catalog() []Metadata {
	return defaultRegistry.Catalog()
}
