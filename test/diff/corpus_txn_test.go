package diff

import "testing"

func txnCorpus() []Scenario {
	return []Scenario{
		{Name: "multi exec", Commands: []Command{
			{Args: []string{"MULTI"}},
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"INCR", "n"}},
			{Args: []string{"EXEC"}},
		}},
		{Name: "discard", Commands: []Command{
			{Args: []string{"MULTI"}},
			{Args: []string{"SET", "k", "v"}},
			{Args: []string{"DISCARD"}},
			{Args: []string{"GET", "k"}},
		}},
		{Name: "exec without multi", Commands: []Command{
			{Args: []string{"EXEC"}},
		}},
		{Name: "queue error aborts exec", Commands: []Command{
			{Args: []string{"MULTI"}},
			{Args: []string{"NOSUCHCMD"}},
			{Args: []string{"EXEC"}},
		}},
	}
}

func TestDifferentialTxn(t *testing.T) {
	hayakvAddr, stop := startHayakv(t)
	defer stop()
	redisAddr, stopR := startRedis8(t)
	defer stopR()
	for _, sc := range txnCorpus() {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			assertReplyEqual(t, sc, runScenario(t, hayakvAddr, sc), runScenario(t, redisAddr, sc))
		})
	}
}
