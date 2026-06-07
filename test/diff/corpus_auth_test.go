package diff

import "testing"

func TestDifferentialAuth(t *testing.T) {
	hayakvAddr, stop := startHayakvAuth(t, "s3cr3t")
	defer stop()
	redisAddr, stopR := startRedis8Auth(t, "s3cr3t")
	defer stopR()
	sc := Scenario{Name: "auth then set get", Commands: []Command{
		{Args: []string{"AUTH", "s3cr3t"}},
		{Args: []string{"SET", "k", "v"}},
		{Args: []string{"GET", "k"}},
	}}
	assertReplyEqual(t, sc, runScenario(t, hayakvAddr, sc), runScenario(t, redisAddr, sc))
}
