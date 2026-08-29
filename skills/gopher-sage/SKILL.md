---
name: gopher-sage
description: Profile-guided Go performance optimization. Use when asked to find, fix, or
  verify CPU or memory/allocation bottlenecks in a running Go service — captures pprof
  profiles with the gopher-sage CLI, reports deterministic findings whose call sites name
  the user's own functions, and after code edits compares before/after reports to confirm
  the bottlenecks are gone.
---

# Optimize a Go service with gopher-sage

gopher-sage is a deterministic pprof analyzer: the same profile always produces the
same report, findings carry stable detector IDs (`CPU-002`, `HEAP-001`, …), and every
finding names the user-code functions its cost flows through. You do the reasoning and
the code edits; the CLI does the measurement.

## Prerequisites (check once, fix if missing)

1. `gopher-sage --version` works. If not: `go install github.com/faizanf47/gopher-sage/cmd/gopher-sage@latest`
2. The target service imports `net/http/pprof` (or mounts its handlers) and is running.
3. The service is receiving **representative load during every capture** — an idle
   profile finds nothing. Ask the user how to generate load if it is not obvious.

## The optimize loop

```sh
# 1. Baseline capture (CPU sampling blocks for the whole --seconds window)
gopher-sage agent capture --server http://localhost:6060 -o .gopher-sage/before

# 2. Findings, ordered by severity
gopher-sage agent report --dir .gopher-sage/before
```

3. **Edit the code.** For each finding, top severity first:
   - `call sites:` names the functions to open and fix — that is the user's own code.
   - `functions:` lists the matched frames (often stdlib/runtime) — evidence, not
     edit targets.
   - `suggestion:` is the canonical remediation pattern; adapt it to the actual code
     rather than applying it verbatim.

4. **Rebuild and RESTART the service** (a restart is required — heap `alloc_*`
   counters accumulate for the life of the process, so without one the after-capture
   inherits the before-capture's allocation history). Re-apply the same load, then:

```sh
# 5. After capture — same --seconds, same kind of load
gopher-sage agent capture --server http://localhost:6060 -o .gopher-sage/after
gopher-sage agent report --dir .gopher-sage/after --json
```

6. **Compare before and after yourself** (see next section), then report the result
   to the user. If bottlenecks remain, loop from step 3.

## Comparing before/after — your job, not the CLI's

Run `agent report --dir <dir> --json` on both directories and join findings by their
`id` field (detector IDs are stable across runs and releases).

- A bottleneck is **fixed** when its detector no longer fires, or when both its
  `share_perc` AND its absolute `matched_value` dropped materially.
- **Shares are relative.** Fixing the top hotspot raises every other finding's share
  even when nothing else changed. Never call something a regression on share alone:
  it must rise in BOTH `share_perc` and `matched_value`, under comparable load and
  the same `--seconds` window.
- Findings appearing or hovering near the 3% noise floor are usually capture
  variance, not real changes.
- Quote absolute values when reporting gains: "regexp: 31% of CPU (412ms) → no longer
  detected; alloc churn at buildPayload: 1.2 GiB → 340 MiB" says more than "31% → 0%".

## Reading a report

- **severity** grades share of profile (high ≥25%, medium ≥10%, low ≥3%); fix high
  first, and do not chase findings under ~5% share.
- **confidence** grades what the profile alone can prove. Low-confidence findings
  (e.g. lock-contention signals on a CPU profile) are leads — corroborate before
  rewriting code.
- `gopher-sage detectors` prints what each detector checks, how it decides, and its
  documented blind spots.
- Add `--top N` to a report for a pprof-style top-functions table when you need
  orientation beyond the findings.

## When to stop

Stop and summarize when any of these holds:
- No high- or medium-severity findings remain.
- The remaining findings are below ~5% share.
- Two consecutive loops produced no measurable improvement — the remaining cost is
  probably essential to the workload; say so rather than forcing changes.

Report what was fixed with before/after numbers, what was left and why, and keep the
`.gopher-sage/` directories so the user can re-verify.
