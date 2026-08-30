package profanalyze

import (
	"reflect"
	"testing"

	pp "github.com/google/pprof/profile"
)

func TestTotals(t *testing.T) {
	t.Parallel()

	heap := &Profile{Raw: heapProfileWithBufferGrowth()}
	want := []SampleTotal{
		{SampleType: "alloc_objects", Unit: "count", Total: 15},
		{SampleType: "alloc_space", Unit: "bytes", Total: 1000},
		{SampleType: "inuse_objects", Unit: "count", Total: 0},
		{SampleType: "inuse_space", Unit: "bytes", Total: 0},
	}
	if got := Totals(heap); !reflect.DeepEqual(got, want) {
		t.Errorf("Totals(heap) = %+v, want %+v", got, want)
	}

	cpu := &Profile{Raw: heavyJSONCPUProfile()}
	want = []SampleTotal{{SampleType: "cpu", Unit: "nanoseconds", Total: 1000}}
	if got := Totals(cpu); !reflect.DeepEqual(got, want) {
		t.Errorf("Totals(cpu) = %+v, want %+v", got, want)
	}
}

// TestTotals_shortValueRow guards the defensive bound: a sample
// carrying fewer values than the profile declares sample types must
// not panic and contributes only the columns it has.
func TestTotals_shortValueRow(t *testing.T) {
	t.Parallel()

	raw := heapProfileWithBufferGrowth()
	raw.Sample = append(raw.Sample, &pp.Sample{
		Location: raw.Sample[0].Location,
		Value:    []int64{5, 100}, // only the two alloc columns
	})
	got := Totals(&Profile{Raw: raw})
	if got[0].Total != 20 || got[1].Total != 1100 {
		t.Errorf("alloc totals = %d/%d, want 20/1100", got[0].Total, got[1].Total)
	}
	if got[3].Total != 0 {
		t.Errorf("inuse_space total = %d, want 0", got[3].Total)
	}
}
