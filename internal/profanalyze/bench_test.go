package profanalyze

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// loadFixtureProfile loads a captured profile fixture, skipping the
// test or benchmark when the fixture is not present in the checkout.
func loadFixtureProfile(tb testing.TB, name string) *Profile {
	tb.Helper()
	path := filepath.Join("..", "..", "fixtures", "profiles", name)
	p, err := Load(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			tb.Skipf("profile fixture %s is not present; skipping", path)
		}
		tb.Fatalf("load fixture %s: %v", path, err)
	}
	return p
}

func BenchmarkParseBytes(b *testing.B) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "profiles", "cpu.pb.gz"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			b.Skip("cpu fixture is not present; skipping")
		}
		b.Fatalf("read fixture: %v", err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseBytes("bench", raw); err != nil {
			b.Fatalf("ParseBytes: %v", err)
		}
	}
}

// BenchmarkBuildView measures the per-view aggregation pass — the
// hot path of the whole analyzer, executed once for a CPU profile
// and four times for a heap profile.
func BenchmarkBuildView(b *testing.B) {
	p := loadFixtureProfile(b, "cpu.pb.gz")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := BuildView(p, SampleCPU); err != nil {
			b.Fatalf("BuildView: %v", err)
		}
	}
}

func BenchmarkRunDetectors(b *testing.B) {
	for _, name := range []string{"cpu.pb.gz", "heap.pb.gz"} {
		b.Run(name, func(b *testing.B) {
			p := loadFixtureProfile(b, name)
			detectors := DefaultDetectors()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Run(p, detectors); err != nil {
					b.Fatalf("Run: %v", err)
				}
			}
		})
	}
}

func BenchmarkTop(b *testing.B) {
	p := loadFixtureProfile(b, "cpu.pb.gz")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Top(p, TopOptions{Limit: 20}); err != nil {
			b.Fatalf("Top: %v", err)
		}
	}
}
