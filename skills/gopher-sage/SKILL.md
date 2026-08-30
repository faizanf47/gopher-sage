---
name: gopher-sage
description: Profile-guided Go performance optimization. Use when asked to find, fix, or
  verify CPU or memory/allocation bottlenecks in a running Go service — captures pprof
  profiles with the gopher-sage CLI, reports deterministic findings whose call sites name
  the user's own functions, and after code edits diffs before/after captures to confirm
  the bottlenecks are gone.
---

# Optimize a Go service with gopher-sage

gopher-sage is a deterministic pprof analyzer: the same profile always produces the
same report, findings carry stable detector IDs (`CPU-002`, `HEAP-001`, …), and every
finding names the user-code functions its cost flows through. You do the reasoning and
the code edits; the CLI does the measurement and the before/after bookkeeping.

## Prerequisites (check once, fix if missing)

1. `gopher-sage --version` works. If not: `go install github.com/faizanf47/gopher-sage/cmd/gopher-sage@latest`
2. The target service imports `net/http/pprof` (or mounts its handlers) and is running.
3. The service is receiving **representative load during every capture** — an idle
   profile finds nothing. Check the project's Makefile/README for existing run and
   load-generation targets before inventing your own.
4. **Control the load.** Before the baseline, check for load generators you did NOT
   start (`ps aux | grep -iE 'hey|wrk|vegeta|bench|traffic'`, running make targets) —
   a stray generator silently doubles the baseline and invalidates every comparison.
   Record the exact load command you use and reuse it **verbatim** for every capture.
   If you cannot control the load, say so explicitly in your final report.

## The optimize loop

```sh
# 1. Baseline capture (cpu, heap and goroutine; CPU blocks for the --seconds window)
gopher-sage agent capture --server http://localhost:6060 -o .gopher-sage/before

# 2. Findings plus the top-functions table — always include --top: the biggest wins
#    are often outside every detector category and only visible there
gopher-sage agent report --dir .gopher-sage/before --top 15
```

3. **Edit the code.** For each finding, top severity first:
   - `call sites:` names the functions to open and fix — that is the user's own code.
   - `functions:` lists the matched frames (often stdlib/runtime) — evidence, not
     edit targets.
   - `suggestion:` is the canonical remediation pattern; adapt it to the actual code.
   - Also scan the `top N functions` table for heavy frames no finding covers.

4. **Preserve behavior.** A semantic-looking swap — regexp → manual parsing,
   `fmt.Sprintf` → `strconv`, a cache-eviction change — needs an equivalence test
   passing BEFORE you claim the win. Performance work is exactly where semantics
   silently drift.

5. **Rebuild and RESTART the service** (required: heap `alloc_*` counters accumulate
   for the life of the process). Re-apply the same load command, then:

```sh
# 6. After capture + mechanical diff (findings joined by detector ID)
gopher-sage agent capture --server http://localhost:6060 -o .gopher-sage/after
gopher-sage agent diff --before .gopher-sage/before --after .gopher-sage/after
```

7. Read the diff (below), report the result with absolute numbers, and loop from
   step 3 while meaningful bottlenecks remain.

## Reading the diff

The labels are mechanical facts, not verdicts — the judgment is yours:

- **fixed / new** — the detector stopped or started firing. A `new` finding near the
  3% noise floor is usually capture variance, not a real regression.
- **improved / worse** — share AND absolute value moved together (±2 pts and ±10%).
  Requiring both is deliberate: shares are relative, so fixing the top hotspot raises
  every other finding's share without anything getting worse.
- **unchanged** — below thresholds, or the two signals disagreed.
- **inconclusive** — an `alloc_*` counter rose. Allocation counters accumulate from
  process start (the note quotes each side's uptime), and the `--seconds` window only
  governs CPU — so a rise may mean nothing but a longer-running process. To settle
  it, run a fixed-work A/B: fresh process, fixed request count, same load, then
  compare `alloc_space` totals. Report it as inconclusive if you cannot.
- Heed the warnings: if CPU sample windows differ, absolute CPU values are not
  comparable — re-capture with matching `--seconds` instead of arguing around it.
- The `goroutine` section diffs totals and top frames: `goroutines: 141053 → 4` is a
  leak fixed; a rising total is a leak found, even though no detector fires on it.

When quoting results, prefer absolutes from the `totals:` lines — "total CPU 7.0s →
1.3s over the same 20s window" says more than any percentage.

## Reading a report

- **severity** grades share of profile (high ≥25%, medium ≥10%, low ≥3%); fix high
  first, and do not chase findings under ~5% share.
- **confidence** grades what the profile alone can prove. Low-confidence findings
  (e.g. lock-contention signals on a CPU profile) are leads — corroborate before
  rewriting code.
- Goroutine/block/mutex profiles appear as summaries (totals + top frames), not
  findings; a large or growing goroutine count is a leak lead.
- `gopher-sage detectors` prints what each detector checks, how it decides, and its
  documented blind spots. `--json` on any subcommand gives the same data structured,
  including per-profile `totals` and `duration_nanos`.

## When to stop

Stop and summarize when any of these holds:
- No high- or medium-severity findings remain, and the goroutine total is stable.
- The remaining findings are below ~5% share.
- Two consecutive loops produced no `improved`/`fixed` labels — the remaining cost is
  probably essential to the workload; say so rather than forcing changes.

Report what was fixed with before/after absolutes, what was left and why, note any
behavior-preservation tests you added, and keep the `.gopher-sage/` directories so
the user can re-run the diff.
