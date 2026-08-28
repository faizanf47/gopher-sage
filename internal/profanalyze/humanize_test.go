package profanalyze

import "testing"

func TestHumanizeValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		v    int64
		unit string
		want string
	}{
		{0, "bytes", "0 B"},
		{512, "bytes", "512 B"},
		{1536, "bytes", "1.5 KiB"},
		{75_030_528, "bytes", "71.6 MiB"},
		{3_221_225_472, "bytes", "3.0 GiB"},
		{2_199_023_255_552, "bytes", "2.0 TiB"},
		{-1536, "bytes", "-1.5 KiB"},
		{812, "nanoseconds", "812ns"},
		{4_200, "nanoseconds", "4.2µs"},
		{4_200_000, "nanoseconds", "4.2ms"},
		{1_500_000_000, "nanoseconds", "1.5s"},
		{42, "count", "42"},
		{42, "", "42"},
		{42, "widgets", "42 widgets"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := humanizeValue(tt.v, tt.unit); got != tt.want {
				t.Errorf("humanizeValue(%d, %q) = %q, want %q", tt.v, tt.unit, got, tt.want)
			}
		})
	}
}
