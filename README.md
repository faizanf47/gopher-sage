# gopher-sage

A deterministic CLI that profiles a running Go service over `net/http/pprof` and reports structured, evidence-backed performance findings. Built on the standard Go toolchain and [github.com/google/pprof](https://github.com/google/pprof) — no LLM, no network calls beyond the pprof fetch, and the same profile always produces the same report.

`gopher-sage` is read-only and advisory. It never modifies code — the engineer reviews each finding and applies the change themselves.

## What it does

Point it at a server's pprof endpoint. It captures the requested profiles (CPU and/or heap), parses them, runs a fixed set of pattern detectors, and prints the findings ordered by severity and share of profile:

```
$ gopher-sage -server http://localhost:6060

gopher-sage report — http://localhost:6060

cpu profile (128412 bytes; sample types: samples, cpu)

  [1] (high severity, high confidence) Regex work dominates CPU
      detector: high-regexp-cpu [CPU-002] — 31.42% of cpu
      evidence: regexp compilation/matching accounts for 31.42% of cpu samples
      functions: regexp.makeOnePass, regexp.compile, regexp.(*Regexp).FindString
      suggestion: hoist regexp.Compile / regexp.MustCompile out of hot paths to package init

  [2] (medium severity, low confidence) Lock-contention signals present
      detector: high-lock-contention-signals [CPU-006] — 12.10% of cpu
      ...

heap profile (34620 bytes; sample types: alloc_objects, alloc_space, inuse_objects, inuse_space)

  [1] (high severity, medium confidence) Buffer growth drives allocations
      ...
```

Each finding cites the detector that fired, the sample type it inspected, its share of the profile, the symbols involved, and a canonical remediation pattern. Confidence is graded honestly: a CPU profile alone cannot *prove* lock contention, so that detector reports a low-confidence lead rather than a verdict.

Pass `-json` to get the same report as structured JSON for scripting or dashboards.

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

Adding a detector: create one file implementing `Detector` (a `Meta() Metadata` describing the rule and a `Detect(DetectCtx) []Finding`), pick the next free number in its scope, and call `MustRegister` from `init()`. The registry rejects duplicate IDs or names and missing metadata at startup.

## Quick start

### 1. Build

```sh
go build -o bin/gopher-sage ./cmd/gopher-sage
# or
make build
```

### 2. Run it against a server with pprof enabled

```sh
./bin/gopher-sage -server http://localhost:6060
```

The target only needs the standard `net/http/pprof` handler mounted. A CPU capture blocks for the whole sample window (30s by default), then the heap capture returns promptly.

### Flags

| Flag | Default | Purpose |
|---|---|---|
| `-server` | — | Base URL of the pprof endpoint (required). `http://host:6060` and `http://host:6060/debug/pprof/` both work |
| `-type` | `cpu,heap` | Comma-separated profile types to capture and analyze |
| `-seconds` | `30` | CPU sample window; `0` uses the server default |
| `-min-share` | `0` | Drop findings below this share-of-profile percent (detectors already apply a 3% noise floor) |
| `-json` | `false` | Emit the report (or catalog) as JSON instead of text |
| `-detectors` | `false` | List the registered detectors — ID, what each checks, how it works, its limitations — and exit |
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
make build              # build the CLI
make leaky-server-start # demo target
```

CI runs `go test ./...` and `make build` against Go 1.25 on every push and PR.

## License

Apache 2.0. See [LICENSE](LICENSE).
