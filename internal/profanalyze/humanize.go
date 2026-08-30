package profanalyze

import (
	"fmt"
	"strconv"
)

// HumanizeValue renders a raw sample value in its pprof unit for
// humans: bytes scale through KiB/MiB/GiB/TiB (base 1024, one
// decimal), nanoseconds through µs/ms/s, counts print plain, and
// unknown units fall back to "<value> <unit>". Deterministic — no
// locale, fixed precision — so report output stays stable.
func HumanizeValue(v int64, unit string) string {
	switch unit {
	case "bytes":
		return humanBytes(v)
	case "nanoseconds":
		return humanDuration(v)
	case "count", "":
		return strconv.FormatInt(v, 10)
	default:
		return fmt.Sprintf("%d %s", v, unit)
	}
}

func humanBytes(v int64) string {
	abs := v
	if abs < 0 {
		abs = -abs
	}
	if abs < 1024 {
		return fmt.Sprintf("%d B", v)
	}
	scaled := float64(v)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		scaled /= 1024
		if scaled > -1024 && scaled < 1024 {
			return fmt.Sprintf("%.1f %s", scaled, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", scaled/1024)
}

func humanDuration(v int64) string {
	abs := v
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs < 1_000:
		return fmt.Sprintf("%dns", v)
	case abs < 1_000_000:
		return fmt.Sprintf("%.1fµs", float64(v)/1_000)
	case abs < 1_000_000_000:
		return fmt.Sprintf("%.1fms", float64(v)/1_000_000)
	default:
		return fmt.Sprintf("%.1fs", float64(v)/1_000_000_000)
	}
}
