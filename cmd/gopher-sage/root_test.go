package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// execCLI runs the full command tree with the given args and returns
// captured stdout and the execution error.
func execCLI(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), err
}

// fixturePath returns the path of a captured profile fixture,
// skipping the test when it is absent from the checkout.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "fixtures", "profiles", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("profile fixture %s is not present; skipping", p)
	}
	return p
}

func TestRoot_helpListsCommands(t *testing.T) {
	t.Parallel()
	stdout, err := execCLI(t)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{"analyze", "detectors", "agent"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("root help missing %q:\n%s", want, stdout)
		}
	}
}

func TestAnalyze_fileMode(t *testing.T) {
	t.Parallel()
	cpu := fixturePath(t, "cpu.pb.gz")
	stdout, err := execCLI(t, "analyze", "--file", cpu, "--top", "3")
	if err != nil {
		t.Fatalf("analyze --file: %v", err)
	}
	for _, want := range []string{"cpu [" + cpu + "] profile", "top 3 functions"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
}

func TestAnalyze_flagValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"no source", []string{"analyze"}, "at least one of the flags"},
		{"both sources", []string{"analyze", "--server", "http://x", "--file", "a.pb.gz"}, "none of the others can be"},
		{"bad type", []string{"analyze", "--server", "http://x", "--type", "goroutine"}, "unsupported profile type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := execCLI(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("err = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestDetectors_catalog(t *testing.T) {
	t.Parallel()
	stdout, err := execCLI(t, "detectors")
	if err != nil {
		t.Fatalf("detectors: %v", err)
	}
	for _, want := range []string{"registered detectors", "CPU-001", "limitations:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("catalog missing %q", want)
		}
	}
}
