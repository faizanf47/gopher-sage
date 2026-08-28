# gopher-sage

A deterministic CLI that profiles a running Go service over `net/http/pprof` and reports structured, evidence-backed performance findings. Built on the standard Go toolchain and [github.com/google/pprof](https://github.com/google/pprof) — no LLM, no network calls beyond the pprof fetch, and the same profile always produces the same report.

`gopher-sage` is read-only and advisory. It never modifies code — the engineer reviews each finding and applies the change themselves.

## What it does

Point it at a server's pprof endpoint — or at saved profile files with `-file` — and it parses the profiles (CPU and/or heap), runs a fixed set of pattern detectors, and prints the findings ordered by severity and share of profile:

```
$ gopher-sage -server http://localhost:6060

gopher-sage report — http://localhost:6060

cpu profile (128412 bytes; sample types: samples, cpu)

  [1] (high severity, high confidence) regexp dominates CPU samples
      detector: high-regexp-cpu [CPU-002] — 31.42% of cpu
      evidence: regexp frames account for 31.42% of CPU (412.0ms of 1.3s).
      functions: regexp.Compile, regexp.MustCompile, regexp.compile, regexp/syntax.Parse
      call sites: main.processItems (28.10%)
      suggestion: regexp.Compile/MustCompile observed on the hot path — almost certainly
                  being compiled per call. Move to a package-level var initialised once.

  [2] (medium severity, low confidence) possible lock / channel contention signal
      detector: high-lock-contention-signals [CPU-006] — 12.10% of cpu
      ...

heap profile (34620 bytes; sample types: alloc_objects, alloc_space, inuse_objects, inuse_space)

  [1] (high severity, high confidence) hot inuse_space frames
      detector: high-inuse-space [HEAP-002] — 97.68% of inuse_space
      evidence: top inuse_space frames account for 97.68% of live heap (283.0 MiB of 289.7 MiB).
      functions: main.buildPayload
      ...
```

Each finding cites the detector that fired, the sample type it inspected, its share of the profile **with the absolute cost humanized** (MiB of heap, ms of CPU), the symbols involved, the **call sites** — the user-code functions the cost flows through, so the report points at *your* function rather than a runtime frame — and a canonical remediation pattern. Confidence is graded honestly: a CPU profile alone cannot *prove* lock contention, so that detector reports a low-confidence lead rather than a verdict.

Pass `-json` to get the same report as structured JSON for scripting or dashboards — findings carry `matched_value` + `unit` (the absolute cost) and `call_sites` (attributed user functions with their profile shares) alongside the fields shown above. `-top N` adds a pprof-style top-N function table to each profile.

## Design at a glance

```
cmd/gopher-sage (flags, wiring)
        │
        ▼
internal/analyze          fetch → parse → detect → report
        │
        ├── internal/profile      typed pprof fetcher (HTTP → bytes)
        └── internal/profanalyze  parser + Top report + ~14 deterministic
                                  CPU/heap pattern detectors
```

The detectors (`internal/profanalyze`) are self-contained rules — CPU regex hot loops, JSON marshalling overhead, GC pressure, heap retention hotspots, unbounded buffer/map growth, and more. Each emits `Finding`s with evidence, share-of-profile, severity, and confidence.

Detectors follow a registry pattern: one detector per file, self-registered into a central registry from `init()`, each with a static ID (`CPU-001`, `HEAP-007`, …) composed from its scope and a number that is never reused. Registration enforces a transparency contract — every detector must publish what it checks, how it decides (frames matched, thresholds applied), and its limitations — and `gopher-sage -detectors` prints that catalog. Findings carry the detector's ID so a report line can always be traced back to the rule and its documented blind spots.

Most detectors are *declarations, not implementations*: a `categorySpec` (the view to read, the frame prefixes that define the category, the report text) handed to a shared engine that owns matching, thresholding, severity grading, call-site attribution, and evidence formatting. Adding one is ~25 declarative lines. Detectors with genuinely unique logic (the cross-view retention detector, for instance) implement `Detector` directly. Either way: one file per detector, the next free number in its scope, `MustRegister` from `init()` — the registry rejects duplicate IDs or names and missing metadata at startup.

## Quick start

### 1. Install or build

```sh
go install github.com/faizanf47/gopher-sage/cmd/gopher-sage@latest
# or, from a checkout:
make build   # produces bin/gopher-sage
```

### 2. Run it against a server with pprof enabled

```sh
./bin/gopher-sage -server http://localhost:6060
```

The target only needs the standard `net/http/pprof` handler mounted. A CPU capture blocks for the whole sample window (30s by default), then the heap capture returns promptly.

### Or analyze saved profiles

```sh
./bin/gopher-sage -file cpu.pb.gz,heap.pb.gz -top 5
```

`-file` takes profiles captured earlier (a production grab, a CI artifact) and infers each file's kind from the sample types it carries — no `-type` needed.

### Flags

| Flag | Default | Purpose |
|---|---|---|
| `-server` | — | Base URL of the pprof endpoint. `http://host:6060` and `http://host:6060/debug/pprof/` both work |
| `-file` | — | Comma-separated saved pprof files to analyze instead of a live server (mutually exclusive with `-server`) |
| `-type` | `cpu,heap` | Comma-separated profile types to capture from `-server` |
| `-seconds` | `30` | CPU sample window; `0` uses the server default |
| `-min-share` | `0` | Drop findings below this share-of-profile percent (detectors already apply a 3% noise floor) |
| `-top` | `0` | Include the top-N functions by cumulative value alongside each profile's findings |
| `-json` | `false` | Emit the report (or catalog) as JSON instead of text |
| `-detectors` | `false` | List the registered detectors — ID, what each checks, how it works, its limitations — and exit |
| `-version` | `false` | Print the version (module version + VCS revision) and exit |
| `-v` | `false` | Verbose logging |

### Try it against the bundled "leaky server"

`fixtures/sources/leaky_server` is a deliberately bad Go program (unbounded cache, goroutine leak, reflect abuse, regex-in-loop, etc.) with pprof enabled on `:6060`. Use it to demo without touching production code.

```sh
make leaky-server-start           # background server on :6060
make leaky-server-traffic &       # generate load in another shell
./bin/gopher-sage -server http://localhost:6060
```

When you are done:

```sh
make leaky-server-stop
```

## Project layout

```
cmd/gopher-sage/         CLI entry point: flags, wiring, output encoding
internal/
  analyze/               pipeline: fetch → parse → detect → render
  profile/               typed pprof fetcher (HTTP → bytes)
  profanalyze/           pprof parser + deterministic CPU/heap detectors
fixtures/
  sources/leaky_server/  deliberately bad Go program for live demos
  profiles/              captured pprof fixtures (cpu/heap/allocs/...)
```

## Development

```sh
go test ./...           # unit tests (deterministic — no network)
go test -race ./...     # same suite under the race detector
golangci-lint run       # curated linter set (see .golangci.yml)
make build              # build the CLI
make leaky-server-start # demo target
```

The parser is fuzzed end to end (arbitrary bytes → parse → detectors → top) and the hot paths are benchmarked against the captured fixtures:

```sh
go test -fuzz=FuzzParseBytes -fuzztime=30s ./internal/profanalyze
go test -bench=. -benchmem -run '^$' ./internal/profanalyze
```

The detector frame lists reference private runtime symbols that Go releases rename freely, so *runtime-truth tests* guard them: real workloads run in-process, a profile is captured from the live runtime via `runtime/pprof`, and the detectors are asserted against it. Two assertions are inverted canaries — Go's heap profiler strips leading `runtime.*` frames from heap samples (`hideRuntime` in `runtime/pprof/protomem.go`), so the all-runtime heap categories must *not* fire on native profiles; if a future Go release changes that, the canary fails and forces a deliberate revisit.

CI runs vet, golangci-lint, the test suite under `-race` with coverage, and both builds against Go 1.25 on every push and PR.

## License

Apache 2.0. See [LICENSE](LICENSE).
