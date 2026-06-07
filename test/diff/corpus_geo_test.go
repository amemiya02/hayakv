package diff

import "testing"

func geoCorpus() []Scenario {
	add := []string{"GEOADD", "Sicily", "13.361389", "38.115556", "Palermo",
		"15.087269", "37.502669", "Catania"}
	return []Scenario{
		{Name: "geodist km", Commands: []Command{
			{Args: add},
			{Args: []string{"GEODIST", "Sicily", "Palermo", "Catania", "km"}},
		}},
		{Name: "geopos", Commands: []Command{
			{Args: add},
			{Args: []string{"GEOPOS", "Sicily", "Palermo", "Catania"}},
		}},
		{Name: "geohash", Commands: []Command{
			{Args: add},
			{Args: []string{"GEOHASH", "Sicily", "Palermo", "Catania"}},
		}},
		{Name: "georadius ordered", Commands: []Command{
			{Args: add},
			{Args: []string{"GEORADIUS", "Sicily", "15", "37", "200", "km", "WITHDIST", "WITHCOORD", "ASC"}},
		}},
		{Name: "georadiusbymember", Commands: []Command{
			{Args: add},
			{Args: []string{"GEORADIUSBYMEMBER", "Sicily", "Palermo", "200", "km"}},
		}},
	}
}

func TestDifferentialGeo(t *testing.T) {
	hayakvAddr, stop := startHayakv(t)
	defer stop()
	redisAddr, stopR := startRedis8(t)
	defer stopR()
	for _, sc := range geoCorpus() {
		sc := sc
		t.Run(sc.Name, func(t *testing.T) {
			assertReplyEqual(t, sc, runScenario(t, hayakvAddr, sc), runScenario(t, redisAddr, sc))
		})
	}
}
