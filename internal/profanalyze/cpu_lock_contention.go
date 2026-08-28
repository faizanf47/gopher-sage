package profanalyze

func init() { MustRegister(newCategoryDetector(cpuLockContentionSpec)) }

// cpuLockContentionSpec reports lock and channel frames on the CPU
// hot path as a possible — never proven — contention signal.
var cpuLockContentionSpec = categorySpec{
	meta: Metadata{
		Scope:  ScopeCPU,
		Num:    6,
		Name:   "high-lock-contention-signals",
		Checks: "Whether lock and channel operations show up on the CPU hot path — a possible contention signal.",
		Method: "Attributes each CPU sample once to the category when any stack " +
			"frame is a sync mutex, runtime semaphore/lock, or channel " +
			"send/receive function, then reports the category's share of total " +
			"CPU. Fires above 3% share; severity is medium at 10% and high at " +
			"25%. Confidence is always low.",
		Limitations: "A CPU profile cannot prove contention: goroutines blocked " +
			"on a lock consume no CPU at all, so real contention is largely " +
			"invisible here, and healthy channel transfers spend CPU in the " +
			"same frames. Only a mutex or block profile can confirm; treat " +
			"this finding strictly as a lead.",
	},
	view: cpuView,
	prefixes: []string{
		"sync.(*Mutex).Lock",
		"sync.(*RWMutex).Lock",
		"sync.(*RWMutex).RLock",
		"runtime.semacquire",
		"runtime.lock",
		"runtime.chansend",
		"runtime.chanrecv",
		"sync.runtime_SemacquireMutex",
	},
	title:      "possible lock / channel contention signal",
	subject:    "sync/runtime lock + channel frames",
	object:     "CPU",
	recommend:  "A CPU profile cannot prove contention by itself. Collect a mutex or block profile to confirm before recommending any change; treat this finding as a hypothesis only.",
	confidence: ConfidenceLow,
}
