package profanalyze

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseBytes throws arbitrary bytes at the parser and, for
// anything that parses, drives the whole downstream pipeline —
// detectors and Top — asserting it never panics and that emitted
// findings honour their structural contract. Seeds come from the
// captured fixture profiles plus a few hostile shapes.
func FuzzParseBytes(f *testing.F) {
	for _, name := range []string{
		"cpu.pb.gz", "heap.pb.gz", "allocs.pb.gz",
		"goroutine.pb.gz", "block.pb.gz", "mutex.pb.gz",
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "profiles", name))
		if err == nil {
			f.Add(raw)
		}
	}
	f.Add([]byte("not a profile"))
	f.Add([]byte{0x1f, 0x8b}) // truncated gzip header
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, raw []byte) {
		p, err := ParseBytes("fuzz", raw)
		if err != nil {
			return // rejecting malformed input is the correct outcome
		}

		_ = p.AvailableSampleTypes()

		findings, err := Run(p, DefaultDetectors())
		if err != nil {
			t.Fatalf("Run on a profile ParseBytes accepted: %v", err)
		}
		for _, fd := range findings {
			if fd.ID == "" || fd.Detector == "" {
				t.Errorf("finding missing ID/Detector: %+v", fd)
			}
		}

		const limit = 5
		top, err := Top(p, TopOptions{Limit: limit})
		if err != nil {
			// A parseable profile can still expose no resolvable
			// sample type; that is an error, not a panic.
			return
		}
		if len(top.Entries) > limit {
			t.Errorf("Top returned %d entries, limit was %d", len(top.Entries), limit)
		}
	})
}
