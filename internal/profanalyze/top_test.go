package profanalyze

import (
	"testing"

	pp "github.com/google/pprof/profile"
)

// stackProfile builds a synthetic profile with three samples that
// share a common root caller. The shape is intentionally similar
// to a real CPU profile: a single sample type, distinct leaf
// frames, and a shared "main.handler" caller threaded through.
//
//	sample[0] = 100 ns: main.handler → main.work
//	sample[1] = 200 ns: main.handler → main.work → encoding/json.Marshal
//	sample[2] =  50 ns: main.handler → main.other
//
// Expected totals (idx=0, "cpu"):
//
//	main.handler          flat=  0 cum=350
//	main.work             flat=100 cum=300
//	encoding/json.Marshal flat=200 cum=200
//	main.other            flat= 50 cum= 50
func stackProfile() *pp.Profile {
	handler := &pp.Function{ID: 1, Name: "main.handler", SystemName: "main.handler", Filename: "main.go"}
	work := &pp.Function{ID: 2, Name: "main.work", SystemName: "main.work", Filename: "main.go"}
	jsonM := &pp.Function{ID: 3, Name: "encoding/json.Marshal", SystemName: "encoding/json.Marshal", Filename: "encoding/json/encode.go"}
	other := &pp.Function{ID: 4, Name: "main.other", SystemName: "main.other", Filename: "main.go"}

	locHandler := &pp.Location{ID: 1, Line: []pp.Line{{Function: handler, Line: 1}}}
	locWork := &pp.Location{ID: 2, Line: []pp.Line{{Function: work, Line: 10}}}
	locJSON := &pp.Location{ID: 3, Line: []pp.Line{{Function: jsonM, Line: 100}}}
	locOther := &pp.Location{ID: 4, Line: []pp.Line{{Function: other, Line: 20}}}

	return &pp.Profile{
		SampleType: []*pp.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &pp.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10_000_000,
		Function:   []*pp.Function{handler, work, jsonM, other},
		Location:   []*pp.Location{locHandler, locWork, locJSON, locOther},
		Sample: []*pp.Sample{
			// pprof orders Sample.Location leaf-first.
			{Location: []*pp.Location{locWork, locHandler}, Value: []int64{100}},
			{Location: []*pp.Location{locJSON, locWork, locHandler}, Value: []int64{200}},
			{Location: []*pp.Location{locOther, locHandler}, Value: []int64{50}},
		},
	}
}

func TestTop_flatCumAndPercentages(t *testing.T) {
	t.Parallel()

	prof := &Profile{Raw: stackProfile()}
	rep, err := Top(prof, TopOptions{SampleIndex: SampleCPU, SortBy: SortByCum, Limit: 20})
	if err != nil {
		t.Fatalf("Top: %v", err)
	}

	if rep.SampleType != string(SampleCPU) {
		t.Fatalf("SampleType = %q, want %q", rep.SampleType, SampleCPU)
	}
	if rep.Total != 350 {
		t.Fatalf("Total = %d, want 350", rep.Total)
	}
	if rep.Unit != "nanoseconds" {
		t.Errorf("Unit = %q, want nanoseconds", rep.Unit)
	}

	got := map[string]TopEntry{}
	for _, e := range rep.Entries {
		got[e.Function] = e
	}

	want := map[string]struct{ flat, cum int64 }{
		"main.handler":          {flat: 0, cum: 350},
		"main.work":             {flat: 100, cum: 300},
		"encoding/json.Marshal": {flat: 200, cum: 200},
		"main.other":            {flat: 50, cum: 50},
	}
	for name, w := range want {
		entry, ok := got[name]
		if !ok {
			t.Errorf("missing entry for %q", name)
			continue
		}
		if entry.Flat != w.flat {
			t.Errorf("%s flat = %d, want %d", name, entry.Flat, w.flat)
		}
		if entry.Cum != w.cum {
			t.Errorf("%s cum = %d, want %d", name, entry.Cum, w.cum)
		}
	}

	// Sorted desc by cum.
	if rep.Entries[0].Function != "main.handler" {
		t.Errorf("first entry = %q, want main.handler", rep.Entries[0].Function)
	}

	// 350 ns total: main.work flat=100 → 28.57%.
	work := got["main.work"]
	if work.FlatPerc < 28.5 || work.FlatPerc > 28.6 {
		t.Errorf("main.work flat%% = %.2f, want ~28.57", work.FlatPerc)
	}
}

func TestTop_sortByFlat(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: stackProfile()}
	rep, err := Top(prof, TopOptions{SampleIndex: SampleCPU, SortBy: SortByFlat, Limit: 20})
	if err != nil {
		t.Fatalf("Top: %v", err)
	}
	// encoding/json.Marshal has flat=200, the largest of the set.
	if rep.Entries[0].Function != "encoding/json.Marshal" {
		t.Errorf("first by flat = %q, want encoding/json.Marshal", rep.Entries[0].Function)
	}
}

func TestTop_limitTruncates(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: stackProfile()}
	rep, err := Top(prof, TopOptions{SampleIndex: SampleCPU, Limit: 2})
	if err != nil {
		t.Fatalf("Top: %v", err)
	}
	if len(rep.Entries) != 2 {
		t.Errorf("len(Entries) = %d, want 2", len(rep.Entries))
	}
}

func TestTop_unknownSampleIndex(t *testing.T) {
	t.Parallel()
	prof := &Profile{Raw: stackProfile()}
	if _, err := Top(prof, TopOptions{SampleIndex: "bogus"}); err == nil {
		t.Fatal("expected error for unknown sample index")
	}
}
