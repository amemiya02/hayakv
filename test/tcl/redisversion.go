package tcl

// RedisImageTag is the frozen Docker image used by the diff harness comparand.
// RedisGitTag is the matching redis/redis git tag the TCL runner checks out.
// Both MUST be the SAME patch (spec §3.1). Bumping these requires an explicit PR
// with a full regression run; implicit drift is forbidden.
const (
	RedisImageTag = "redis:8.4.2"
	RedisGitTag   = "8.4.2"
)
