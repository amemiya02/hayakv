package diff

import "testing"

func scanCorpus() []Scenario {
	return []Scenario{
		{Name: "scan basic", Commands: []Command{
			{Args: []string{"SET", "a", "1"}},
			{Args: []string{"SET", "b", "2"}},
			{Args: []string{"SET", "c", "3"}},
			{Args: []string{"SCAN", "0", "COUNT", "1000"}, Normalize: normalizeScan},
		}},
	}
}

func TestDifferentialScan(t *testing.T) {
	hayakvAddr, stop := startHayakv(t)
	defer stop()
	redisAddr, stopR := startRedis8(t)
	defer stopR()
	for _, sc := range scanCorpus() {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			assertReplyEqual(t, sc, runScenario(t, hayakvAddr, sc), runScenario(t, redisAddr, sc))
		})
	}
}
