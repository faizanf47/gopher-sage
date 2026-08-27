// Package profanalyze turns a captured Go pprof profile into
// JSON-friendly, deterministic facts a human (or any downstream
// consumer) can act on directly.
//
// The package has three concerns, kept in separate files:
//
//   - parser.go — open a captured pprof file (or raw bytes), parse
//     it with github.com/google/pprof/profile, validate it, and
//     resolve the sample type (CPU vs heap views) the caller asked
//     for.
//   - top.go    — compute a structured Top report (flat / cum /
//     flat% / cum% per function) for the resolved sample index.
//   - detect.go — run deterministic pattern detectors over the
//     parsed profile and emit structured findings.
//
// The package never shells out to `go tool pprof`. Inputs are
// validated up front; outputs are plain data types safe to serialise.
package profanalyze

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	pp "github.com/google/pprof/profile"
)

// SampleIndex names the view of a profile the caller wants. The
// names match the canonical pprof sample-type names so a string
// passed through from the tool surface goes straight in.
type SampleIndex string

const (
	// SampleCPU selects the CPU sample type. Some Go CPU profiles
	// expose it as "cpu" (nanoseconds), others as "samples" (count) —
	// the loader accepts either alias.
	SampleCPU SampleIndex = "cpu"
	// SampleSamples is the count-based alias of SampleCPU.
	SampleSamples SampleIndex = "samples"

	// SampleInuseSpace selects live heap bytes. A heap profile
	// carries this and the three sample types below.
	SampleInuseSpace SampleIndex = "inuse_space"
	// SampleInuseObjects selects live heap object counts.
	SampleInuseObjects SampleIndex = "inuse_objects"
	// SampleAllocSpace selects cumulative allocated bytes.
	SampleAllocSpace SampleIndex = "alloc_space"
	// SampleAllocObjects selects cumulative allocated object counts.
	SampleAllocObjects SampleIndex = "alloc_objects"
)

// Profile bundles the parsed *pprof.Profile with the path it was
// loaded from. The path is retained for diagnostics only.
type Profile struct {
	Path string
	Raw  *pp.Profile
}

// Load reads a pprof file from disk, parses it, and runs CheckValid
// before returning. pprof.Parse already invokes CheckValid, but we
// keep the explicit call so a future switch to ParseUncompressed
// (which does not) does not silently regress validation.
func Load(path string) (*Profile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("profanalyze: path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("profanalyze: open %q: %w", path, err)
	}
	// The file is read-only, so a close error carries no signal.
	defer func() { _ = f.Close() }()

	raw, err := pp.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("profanalyze: parse %q: %w", path, err)
	}
	if err := raw.CheckValid(); err != nil {
		return nil, fmt.Errorf("profanalyze: invalid profile %q: %w", path, err)
	}
	return &Profile{Path: path, Raw: raw}, nil
}

// ParseBytes parses a pprof profile from raw bytes, as returned by
// an HTTP fetch of a /debug/pprof endpoint. src labels the origin
// (a URL, typically) for diagnostics only.
func ParseBytes(src string, raw []byte) (*Profile, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("profanalyze: empty profile from %q", src)
	}
	p, err := pp.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("profanalyze: parse profile from %q: %w", src, err)
	}
	if err := p.CheckValid(); err != nil {
		return nil, fmt.Errorf("profanalyze: invalid profile from %q: %w", src, err)
	}
	return &Profile{Path: src, Raw: p}, nil
}

// HasCPUSamples reports whether the profile carries a CPU sample
// type. Go CPU profiles expose it as "cpu" (nanoseconds) or
// "samples" (count) depending on how they were captured.
func (p *Profile) HasCPUSamples() bool {
	return hasSampleType(p, "cpu", "samples")
}

// HasHeapSamples reports whether the profile carries any of the four
// heap sample types (inuse/alloc × space/objects).
func (p *Profile) HasHeapSamples() bool {
	return hasSampleType(p, "inuse_space", "alloc_space", "inuse_objects", "alloc_objects")
}

// AvailableSampleTypes returns the sample-type names carried by the
// profile, in profile order. Useful for diagnostics when the caller
// asks for an index the profile does not expose.
func (p *Profile) AvailableSampleTypes() []string {
	out := make([]string, 0, len(p.Raw.SampleType))
	for _, st := range p.Raw.SampleType {
		out = append(out, st.Type)
	}
	return out
}

// ResolveSampleIndex maps a user-facing SampleIndex onto the column
// inside Sample.Value that holds the corresponding numeric.
//
// When name is empty the resolver picks a sensible default:
//
//   - heap profiles → inuse_space.
//   - everything else → the profile's DefaultSampleType, or its
//     first column if no default is set.
//
// CPU profiles expose either "samples" or "cpu" depending on how
// they were captured; either alias resolves to whichever the
// profile actually carries.
func (p *Profile) ResolveSampleIndex(name SampleIndex) (idx int, resolved SampleIndex, unit string, err error) {
	if len(p.Raw.SampleType) == 0 {
		return -1, "", "", fmt.Errorf("profanalyze: profile has no sample types")
	}

	want := strings.TrimSpace(strings.ToLower(string(name)))

	// Empty input picks the right default for the kind of profile.
	if want == "" {
		if i := indexOfSampleType(p.Raw.SampleType, "inuse_space"); i >= 0 {
			return i, SampleInuseSpace, p.Raw.SampleType[i].Unit, nil
		}
		if def := strings.TrimSpace(p.Raw.DefaultSampleType); def != "" {
			if i := indexOfSampleType(p.Raw.SampleType, def); i >= 0 {
				return i, SampleIndex(def), p.Raw.SampleType[i].Unit, nil
			}
		}
		return 0, SampleIndex(p.Raw.SampleType[0].Type), p.Raw.SampleType[0].Unit, nil
	}

	// "cpu" and "samples" are aliases — match whichever the profile
	// actually carries.
	if want == string(SampleCPU) || want == string(SampleSamples) {
		for _, alias := range []string{string(SampleCPU), string(SampleSamples)} {
			if i := indexOfSampleType(p.Raw.SampleType, alias); i >= 0 {
				return i, SampleIndex(alias), p.Raw.SampleType[i].Unit, nil
			}
		}
		return -1, "", "", fmt.Errorf(
			"profanalyze: profile has no %q or %q sample type; available: %v",
			SampleCPU, SampleSamples, p.AvailableSampleTypes(),
		)
	}

	if i := indexOfSampleType(p.Raw.SampleType, want); i >= 0 {
		return i, SampleIndex(want), p.Raw.SampleType[i].Unit, nil
	}
	return -1, "", "", fmt.Errorf(
		"profanalyze: profile has no %q sample type; available: %v",
		want, p.AvailableSampleTypes(),
	)
}

func indexOfSampleType(types []*pp.ValueType, name string) int {
	for i, st := range types {
		if st.Type == name {
			return i
		}
	}
	return -1
}
