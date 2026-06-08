package diff

import (
	"bytes"
	"testing"
)

func censusCorpus() []Scenario {
	return []Scenario{
		{
			Name: "lpos",
			Commands: []Command{
				{Args: []string{"RPUSH", "l", "a", "b", "c", "b"}},
				{Args: []string{"LPOS", "l", "b"}},
				{Args: []string{"LPOS", "l", "b", "RANK", "-1"}},
				{Args: []string{"LPOS", "l", "b", "COUNT", "0"}},
			},
		},
		{
			Name: "smismember",
			Commands: []Command{
				{Args: []string{"SADD", "s", "a", "b", "c"}},
				{Args: []string{"SMISMEMBER", "s", "a", "x", "b"}},
			},
		},
		{
			Name: "sintercard",
			Commands: []Command{
				{Args: []string{"SADD", "s1", "a", "b", "c"}},
				{Args: []string{"SADD", "s2", "b", "c", "d"}},
				{Args: []string{"SINTERCARD", "2", "s1", "s2"}},
				{Args: []string{"SINTERCARD", "2", "s1", "s2", "LIMIT", "1"}},
			},
		},
		{
			Name: "lmpop",
			Commands: []Command{
				{Args: []string{"RPUSH", "l1", "a", "b", "c"}},
				{Args: []string{"LMPOP", "1", "l1", "LEFT", "COUNT", "2"}},
			},
		},
		{
			Name: "zmpop",
			Commands: []Command{
				{Args: []string{"ZADD", "z", "1", "a", "2", "b", "3", "c"}},
				{Args: []string{"ZMPOP", "1", "z", "MIN", "COUNT", "2"}},
			},
		},
	}
}

func TestDifferentialCensus(t *testing.T) {
	hayakvAddr, stopHayakv := startHayakvExtraConfig(t, "")
	defer stopHayakv()
	redisAddr, stopRedis := startRedis8(t)
	defer stopRedis()

	for _, scenario := range censusCorpus() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			hayakvReplies := runScenario(t, hayakvAddr, scenario)
			redisReplies := runScenario(t, redisAddr, scenario)
			if len(hayakvReplies) != len(redisReplies) {
				t.Fatalf("reply count hayakv=%d redis=%d", len(hayakvReplies), len(redisReplies))
			}
			for i := range hayakvReplies {
				hReply, rReply := hayakvReplies[i], redisReplies[i]
				if fn := scenario.Commands[i].Normalize; fn != nil {
					hReply = fn(hReply)
					rReply = fn(rReply)
				}
				if !bytes.Equal(hReply, rReply) {
					t.Fatalf("command %v\nhayakv: %q\nredis:  %q",
						scenario.Commands[i].Args, hReply, rReply)
				}
			}
		})
	}
}
