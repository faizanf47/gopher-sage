package profanalyze

import (
	"reflect"
	"testing"
)

func TestIsUserFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		frame string
		want  bool
	}{
		{"main.handler", true},
		{"main.(*server).handleWork", true},
		{"github.com/faizanf47/gopher-sage/internal/analyze.Run", true},
		{"golang.org/x/sync/errgroup.(*Group).Go", true},
		{"runtime.mallocgc", false},
		{"bytes.(*Buffer).grow", false},
		{"encoding/json.Marshal", false},
		{"net/http.HandlerFunc.ServeHTTP", false},
		{"internal/runtime/maps.newTable", false},
		{"regexp/syntax.(*compiler).inst", false},
		{"(unknown)", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isUserFrame(tt.frame); got != tt.want {
			t.Errorf("isUserFrame(%q) = %v, want %v", tt.frame, got, tt.want)
		}
	}
}

// viewFromStacks builds a View directly from leaf-first stacks, the
// way BuildView would, for exercising matchCategory in isolation.
func viewFromStacks(stacks []SampleStack) View {
	v := View{
		SampleType: "cpu",
		Unit:       "nanoseconds",
		FlatByFn:   map[string]int64{},
		CumByFn:    map[string]int64{},
		Samples:    stacks,
	}
	for _, s := range stacks {
		v.Total += s.Value
		for _, frame := range s.Frames {
			v.CumByFn[frame] += s.Value
		}
	}
	return v
}

func TestMatchCategory_callSiteAttribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stacks    []SampleStack
		prefixes  []string
		exclude   []string
		wantValue int64
		wantSites []CallSite
	}{
		{
			name: "nearest user caller above deepest match",
			stacks: []SampleStack{
				{Value: 700, Frames: []string{"regexp.compile", "regexp.MustCompile", "main.setup"}},
				{Value: 300, Frames: []string{"main.other"}},
			},
			prefixes:  []string{"regexp."},
			wantValue: 700,
			wantSites: []CallSite{{Function: "main.setup", SharePerc: 70}},
		},
		{
			name: "intermediate stdlib frames are skipped",
			stacks: []SampleStack{
				{Value: 500, Frames: []string{"runtime.concatstrings", "fmt.Sprintf", "main.format"}},
				{Value: 500, Frames: []string{"main.other"}},
			},
			prefixes:  []string{"runtime.concatstring"},
			wantValue: 500,
			wantSites: []CallSite{{Function: "main.format", SharePerc: 50}},
		},
		{
			name: "user callback under stdlib gets the credit",
			stacks: []SampleStack{
				{Value: 1000, Frames: []string{
					"runtime.concatstrings",
					"main.(*T).MarshalJSON",
					"encoding/json.Marshal",
					"main.handler",
				}},
			},
			prefixes:  []string{"runtime.concatstring"},
			wantValue: 1000,
			wantSites: []CallSite{{Function: "main.(*T).MarshalJSON", SharePerc: 100}},
		},
		{
			name: "pure-runtime stack counts toward value but no site",
			stacks: []SampleStack{
				{Value: 600, Frames: []string{"runtime.mallocgc", "runtime.gcBgMarkWorker"}},
				{Value: 400, Frames: []string{"runtime.mallocgc", "main.alloc"}},
			},
			prefixes:  []string{"runtime.mallocgc"},
			wantValue: 1000,
			wantSites: []CallSite{{Function: "main.alloc", SharePerc: 40}},
		},
		{
			name: "cap at three with deterministic tie-break",
			stacks: []SampleStack{
				{Value: 40, Frames: []string{"reflect.ValueOf", "main.a"}},
				{Value: 30, Frames: []string{"reflect.ValueOf", "main.b"}},
				{Value: 10, Frames: []string{"reflect.ValueOf", "main.d"}},
				{Value: 10, Frames: []string{"reflect.ValueOf", "main.c"}},
				{Value: 10, Frames: []string{"main.other"}},
			},
			prefixes:  []string{"reflect."},
			wantValue: 90,
			wantSites: []CallSite{
				{Function: "main.a", SharePerc: 40},
				{Function: "main.b", SharePerc: 30},
				{Function: "main.c", SharePerc: 10},
			},
		},
		{
			name: "excluded frame does not match at all",
			stacks: []SampleStack{
				{Value: 900, Frames: []string{"runtime.slicebytetostringtmp", "main.fast"}},
				{Value: 100, Frames: []string{"runtime.slicebytetostring", "main.slow"}},
			},
			prefixes:  []string{"runtime.slicebytetostring"},
			exclude:   []string{"runtime.slicebytetostringtmp"},
			wantValue: 100,
			wantSites: []CallSite{{Function: "main.slow", SharePerc: 10}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := viewFromStacks(tt.stacks)
			m := matchCategory(v, tt.prefixes, tt.exclude)
			if m.value != tt.wantValue {
				t.Errorf("value = %d, want %d", m.value, tt.wantValue)
			}
			if !reflect.DeepEqual(m.callSites, tt.wantSites) {
				t.Errorf("callSites = %+v, want %+v", m.callSites, tt.wantSites)
			}
		})
	}
}
