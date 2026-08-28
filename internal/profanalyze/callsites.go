package profanalyze

import (
	"sort"
	"strings"
)

// maxCallSites caps Finding.CallSites so reports stay compact even
// when a category's cost flows through many callers.
const maxCallSites = 3

// CallSite is one user-code caller a category's cost is attributed
// to, with its share of the view total. Call-site shares use the
// same denominator as the finding's SharePerc, so they are directly
// comparable; they can sum to less than the finding share because
// samples whose whole stack is runtime code carry no call site.
type CallSite struct {
	Function  string  `json:"function"`
	SharePerc float64 `json:"share_perc"`
}

// isUserFrame reports whether a frame names code outside the Go
// standard library and runtime. Frames with a '/' are user code iff
// the first path segment contains a '.' (module domains like
// github.com); frames without a '/' are user code only for package
// main — so bytes.(*Buffer).grow and runtime.mallocgc classify as
// stdlib. Third-party dependencies classify as user code: naming the
// library a cost flows through is still far more actionable than
// naming a runtime frame. A dotless single-segment module path (rare
// outside replace directives) misclassifies as stdlib — accepted.
func isUserFrame(name string) bool {
	if name == "" || name == "(unknown)" {
		return false
	}
	head, _, hasSlash := strings.Cut(name, "/")
	if hasSlash {
		return strings.Contains(head, ".")
	}
	return strings.HasPrefix(name, "main.")
}

// topCallSites ranks the per-caller attribution map by value (ties
// by name, for deterministic output), caps it at n entries, and
// converts values to shares of the view total.
func topCallSites(byCaller map[string]int64, total int64, n int) []CallSite {
	if len(byCaller) == 0 {
		return nil
	}
	type callerValue struct {
		name  string
		value int64
	}
	callers := make([]callerValue, 0, len(byCaller))
	for name, value := range byCaller {
		callers = append(callers, callerValue{name, value})
	}
	sort.Slice(callers, func(i, j int) bool {
		if callers[i].value != callers[j].value {
			return callers[i].value > callers[j].value
		}
		return callers[i].name < callers[j].name
	})
	if n < len(callers) {
		callers = callers[:n]
	}
	sites := make([]CallSite, 0, len(callers))
	for _, c := range callers {
		sites = append(sites, CallSite{
			Function:  c.name,
			SharePerc: roundPerc(percentOf(c.value, total)),
		})
	}
	return sites
}
