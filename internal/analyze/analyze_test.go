package analyze

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	pp "github.com/google/pprof/profile"

	"github.com/faizanf47/gopher-sage/internal/profanalyze"
	"github.com/faizanf47/gopher-sage/internal/profile"
)

// marshalProfile serialises a synthetic profile into the compressed
// wire format a /debug/pprof endpoint returns.
func marshalProfile(t *testing.T, p *pp.Profile) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	return buf.Bytes()
}

// heavyJSONCPUProfile builds a CPU profile where ~70% of samples run
// through encoding/json.Marshal — enough to fire the JSON detector
// at high severity.
func heavyJSONCPUProfile() *pp.Profile {
	handler := &pp.Function{ID: 1, Name: "main.handler", Filename: "main.go"}
	jsonM := &pp.Function{ID: 2, Name: "encoding/json.Marshal", Filename: "encoding/json/encode.go"}
	other := &pp.Function{ID: 3, Name: "main.other", Filename: "main.go"}

	locHandler := &pp.Location{ID: 1, Line: []pp.Line{{Function: handler, Line: 1}}}
	locJSON := &pp.Location{ID: 2, Line: []pp.Line{{Function: jsonM, Line: 100}}}
	locOther := &pp.Location{ID: 3, Line: []pp.Line{{Function: other, Line: 20}}}

	return &pp.Profile{
		SampleType: []*pp.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &pp.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10_000_000,
		Function:   []*pp.Function{handler, jsonM, other},
		Location:   []*pp.Location{locHandler, locJSON, locOther},
		Sample: []*pp.Sample{
			{Location: []*pp.Location{locJSON, locHandler}, Value: []int64{700}},
			{Location: []*pp.Location{locOther, locHandler}, Value: []int64{300}},
		},
	}
}

// heapProfileWithBufferGrowth builds a heap profile where
// runtime.growslice + bytes.makeSlice drive alloc_space — the
// signature the buffer-growth detector recognises.
func heapProfileWithBufferGrowth() *pp.Profile {
	handler := &pp.Function{ID: 1, Name: "main.handler", Filename: "main.go"}
	grow := &pp.Function{ID: 2, Name: "runtime.growslice", Filename: "runtime/slice.go"}
	bytesMake := &pp.Function{ID: 3, Name: "bytes.makeSlice", Filename: "bytes/buffer.go"}

	locHandler := &pp.Location{ID: 1, Line: []pp.Line{{Function: handler, Line: 1}}}
	locGrow := &pp.Location{ID: 2, Line: []pp.Line{{Function: grow, Line: 10}}}
	locBytes := &pp.Location{ID: 3, Line: []pp.Line{{Function: bytesMake, Line: 10}}}

	return &pp.Profile{
		SampleType: []*pp.ValueType{
			{Type: "alloc_objects", Unit: "count"},
			{Type: "alloc_space", Unit: "bytes"},
			{Type: "inuse_objects", Unit: "count"},
			{Type: "inuse_space", Unit: "bytes"},
		},
		PeriodType:        &pp.ValueType{Type: "space", Unit: "bytes"},
		Period:            524288,
		DefaultSampleType: "inuse_space",
		Function:          []*pp.Function{handler, grow, bytesMake},
		Location:          []*pp.Location{locHandler, locGrow, locBytes},
		Sample: []*pp.Sample{
			{Location: []*pp.Location{locGrow, locHandler}, Value: []int64{10, 800, 0, 0}},
			{Location: []*pp.Location{locBytes, locHandler}, Value: []int64{5, 200, 0, 0}},
		},
	}
}

// stubFetcher serves canned profile bytes keyed by profile type.
type stubFetcher struct {
	byType map[profile.Type][]byte
	err    error
}

func (s stubFetcher) Fetch(_ context.Context, src profile.Source) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	raw, ok := s.byType[src.Type]
	if !ok {
		return nil, fmt.Errorf("no stub profile for type %q", src.Type)
	}
	return raw, nil
}

func TestRun_cpuAndHeap(t *testing.T) {
	t.Parallel()

	fetcher := stubFetcher{byType: map[profile.Type][]byte{
		profile.TypeCPU:  marshalProfile(t, heavyJSONCPUProfile()),
		profile.TypeHeap: marshalProfile(t, heapProfileWithBufferGrowth()),
	}}

	rep, err := Run(context.Background(), fetcher, Options{
		Server: "http://localhost:6060",
		Types:  []profile.Type{profile.TypeCPU, profile.TypeHeap},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if rep.Server != "http://localhost:6060" {
		t.Errorf("Server = %q, want %q", rep.Server, "http://localhost:6060")
	}
	if len(rep.Profiles) != 2 {
		t.Fatalf("got %d profile reports, want 2", len(rep.Profiles))
	}

	cpu, heap := rep.Profiles[0], rep.Profiles[1]
	if cpu.Type != profile.TypeCPU || heap.Type != profile.TypeHeap {
		t.Fatalf("profile order = %s, %s; want cpu, heap", cpu.Type, heap.Type)
	}
	if cpu.Bytes == 0 || heap.Bytes == 0 {
		t.Errorf("expected non-zero byte counts, got cpu=%d heap=%d", cpu.Bytes, heap.Bytes)
	}
	if !hasDetector(cpu.Findings, "high-json-cpu") {
		t.Errorf("cpu findings missing high-json-cpu: %+v", cpu.Findings)
	}
	if len(heap.Findings) == 0 {
		t.Errorf("expected heap findings, got none")
	}
	for _, pr := range rep.Profiles {
		assertSeverityOrdered(t, pr.Findings)
	}
}

func TestRun_minShareFiltersFindings(t *testing.T) {
	t.Parallel()

	fetcher := stubFetcher{byType: map[profile.Type][]byte{
		profile.TypeCPU: marshalProfile(t, heavyJSONCPUProfile()),
	}}

	rep, err := Run(context.Background(), fetcher, Options{
		Server:   "http://localhost:6060",
		Types:    []profile.Type{profile.TypeCPU},
		MinShare: 99.9,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := rep.Profiles[0].Findings; len(got) != 0 {
		t.Errorf("expected all findings filtered at min share 99.9, got %+v", got)
	}
}

func TestRun_fetchErrorAborts(t *testing.T) {
	t.Parallel()

	fetchErr := errors.New("connection refused")
	_, err := Run(context.Background(), stubFetcher{err: fetchErr}, Options{
		Server: "http://localhost:6060",
		Types:  []profile.Type{profile.TypeCPU},
	})
	if !errors.Is(err, fetchErr) {
		t.Fatalf("err = %v, want wrapped %v", err, fetchErr)
	}
	if !strings.Contains(err.Error(), "analyze cpu profile") {
		t.Errorf("err = %v, want the failing profile type in the message", err)
	}
}

func TestOptions_validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{
			name:    "missing server",
			opts:    Options{Types: []profile.Type{profile.TypeCPU}},
			wantErr: "server URL is required",
		},
		{
			name:    "no types",
			opts:    Options{Server: "http://localhost:6060"},
			wantErr: "at least one profile type",
		},
		{
			name: "unsupported type",
			opts: Options{
				Server: "http://localhost:6060",
				Types:  []profile.Type{profile.TypeGoroutine},
			},
			wantErr: "unsupported profile type",
		},
		{
			name: "negative seconds",
			opts: Options{
				Server:  "http://localhost:6060",
				Types:   []profile.Type{profile.TypeCPU},
				Seconds: -1,
			},
			wantErr: "seconds must be non-negative",
		},
		{
			name: "negative min share",
			opts: Options{
				Server:   "http://localhost:6060",
				Types:    []profile.Type{profile.TypeCPU},
				MinShare: -1,
			},
			wantErr: "min share must be non-negative",
		},
		{
			name: "valid",
			opts: Options{
				Server: "http://localhost:6060",
				Types:  []profile.Type{profile.TypeCPU, profile.TypeHeap},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.opts.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validate: unexpected error %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestWriteText(t *testing.T) {
	t.Parallel()

	rep := Report{
		Server: "http://localhost:6060",
		Profiles: []ProfileReport{
			{
				Type:        profile.TypeCPU,
				Bytes:       1234,
				SampleTypes: []string{"cpu"},
				Findings: []profanalyze.Finding{{
					Detector:       "high-json-cpu",
					Scope:          profanalyze.ScopeCPU,
					Title:          "encoding/json dominates CPU",
					Evidence:       "70.00% of cpu in encoding/json frames",
					Recommendation: "reduce marshalling in the hot path",
					SampleType:     "cpu",
					SharePerc:      70,
					Severity:       profanalyze.SeverityHigh,
					Confidence:     profanalyze.ConfidenceHigh,
					Functions:      []string{"encoding/json.Marshal"},
				}},
			},
			{
				Type:        profile.TypeHeap,
				Bytes:       567,
				SampleTypes: []string{"alloc_space", "inuse_space"},
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteText(&buf, rep); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"http://localhost:6060",
		"cpu profile (1234 bytes",
		"high severity, high confidence",
		"encoding/json dominates CPU",
		"high-json-cpu — 70.00% of cpu",
		"suggestion: reduce marshalling in the hot path",
		"no findings above the share threshold",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSortFindings(t *testing.T) {
	t.Parallel()

	in := []profanalyze.Finding{
		{Detector: "b", Severity: profanalyze.SeverityLow, SharePerc: 90},
		{Detector: "a", Severity: profanalyze.SeverityHigh, SharePerc: 10},
		{Detector: "c", Severity: profanalyze.SeverityHigh, SharePerc: 40},
		{Detector: "d", Severity: profanalyze.SeverityMedium, SharePerc: 40},
	}
	sortFindings(in)

	var got []string
	for _, f := range in {
		got = append(got, f.Detector)
	}
	want := []string{"c", "a", "d", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func hasDetector(in []profanalyze.Finding, name string) bool {
	for _, f := range in {
		if f.Detector == name {
			return true
		}
	}
	return false
}

func assertSeverityOrdered(t *testing.T, in []profanalyze.Finding) {
	t.Helper()
	for i := 1; i < len(in); i++ {
		if severityRank(in[i].Severity) > severityRank(in[i-1].Severity) {
			t.Errorf("findings not ordered by severity: %+v", in)
			return
		}
	}
}
