package diff

import "testing"

func evalCorpus() []Scenario {
	return []Scenario{
		{Name: "eval return types", Commands: []Command{
			{Args: []string{"EVAL", "return 1", "0"}},
			{Args: []string{"EVAL", "return 'str'", "0"}},
			{Args: []string{"EVAL", "return {1,2,3}", "0"}},
			{Args: []string{"EVAL", "return true", "0"}},
			{Args: []string{"EVAL", "return false", "0"}},
			{Args: []string{"EVAL", "return redis.error_reply('boom')", "0"}},
			{Args: []string{"EVAL", "return redis.status_reply('GOOD')", "0"}},
		}},
		{Name: "eval call set get", Commands: []Command{
			{Args: []string{"EVAL", "redis.call('set',KEYS[1],ARGV[1]) return redis.call('get',KEYS[1])", "1", "k", "v"}},
		}},
		{Name: "eval sha1hex", Commands: []Command{
			{Args: []string{"EVAL", "return redis.sha1hex('')", "0"}},
		}},
		{Name: "evalsha noscript", Commands: []Command{
			{Args: []string{"EVALSHA", "ffffffffffffffffffffffffffffffffffffffff", "0"}},
		}},
		{Name: "script load exists", Commands: []Command{
			{Args: []string{"SCRIPT", "LOAD", "return 1"}},
			{Args: []string{"SCRIPT", "EXISTS", "e0e1f9ca3a614684e9023f2def1c8e34316f6e30"}},
		}},
	}
}

func TestDifferentialEval(t *testing.T) {
	hayakvAddr, stop := startHayakv(t)
	defer stop()
	redisAddr, stopR := startRedis8(t)
	defer stopR()
	for _, sc := range evalCorpus() {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			assertReplyEqual(t, sc, runScenario(t, hayakvAddr, sc), runScenario(t, redisAddr, sc))
		})
	}
}
