package main

import (
	"reflect"
	"testing"

	"github.com/faizanf47/gopher-sage/internal/profile"
)

func TestParseTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    []profile.Type
		wantErr bool
	}{
		{
			name: "default value",
			in:   "cpu,heap",
			want: []profile.Type{profile.TypeCPU, profile.TypeHeap},
		},
		{
			name: "order preserved",
			in:   "heap,cpu",
			want: []profile.Type{profile.TypeHeap, profile.TypeCPU},
		},
		{
			name: "whitespace and case normalised",
			in:   " CPU , Heap ",
			want: []profile.Type{profile.TypeCPU, profile.TypeHeap},
		},
		{
			name: "trailing comma ignored",
			in:   "cpu,",
			want: []profile.Type{profile.TypeCPU},
		},
		{
			name:    "unsupported type",
			in:      "cpu,goroutine",
			wantErr: true,
		},
		{
			name:    "empty",
			in:      "",
			wantErr: true,
		},
		{
			name:    "only separators",
			in:      ", ,",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTypes(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseTypes(%q): want error, got %v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTypes(%q): %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTypes(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want []string
	}{
		{in: "a,b", want: []string{"a", "b"}},
		{in: " a , b ", want: []string{"a", "b"}},
		{in: "a,,b,", want: []string{"a", "b"}},
		{in: "", want: nil},
		{in: " , ", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := splitList(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitList(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
