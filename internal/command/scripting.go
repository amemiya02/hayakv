package database

import (
	"strconv"
	"strings"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func parseEvalArgs(args [][]byte) (script string, keys, argv []string, errReply redis.Reply) {
	if len(args) < 2 {
		return "", nil, nil, protocol.MakeArgNumErrReply("eval")
	}
	script = string(args[0])
	numKeys, e := strconv.Atoi(string(args[1]))
	if e != nil || numKeys < 0 {
		return "", nil, nil, protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	if 2+numKeys > len(args) {
		return "", nil, nil, protocol.MakeErrReply("ERR Number of keys can't be greater than number of args")
	}
	for _, k := range args[2 : 2+numKeys] {
		keys = append(keys, string(k))
	}
	for _, a := range args[2+numKeys:] {
		argv = append(argv, string(a))
	}
	return script, keys, argv, nil
}

func (server *Server) execEval(c redis.Connection, args [][]byte, readonly bool) redis.Reply {
	body, keys, argv, errReply := parseEvalArgs(args)
	if errReply != nil {
		return errReply
	}
	return server.scriptEngine.Eval(c, body, keys, argv, readonly)
}

func (server *Server) execEvalSha(c redis.Connection, args [][]byte, readonly bool) redis.Reply {
	_, keys, argv, errReply := parseEvalArgs(args)
	if errReply != nil {
		return errReply
	}
	return server.scriptEngine.EvalSha(c, strings.ToLower(string(args[0])), keys, argv, readonly)
}

func (server *Server) execScript(c redis.Connection, args [][]byte) redis.Reply {
	if len(args) == 0 {
		return protocol.MakeArgNumErrReply("script")
	}
	switch strings.ToUpper(string(args[0])) {
	case "LOAD":
		if len(args) != 2 {
			return protocol.MakeArgNumErrReply("script|load")
		}
		return protocol.MakeBulkReply([]byte(server.scriptEngine.Load(string(args[1]))))
	case "EXISTS":
		shas := make([]string, 0, len(args)-1)
		for _, s := range args[1:] {
			shas = append(shas, strings.ToLower(string(s)))
		}
		out := make([]redis.Reply, 0, len(shas))
		for _, b := range server.scriptEngine.Exists(shas) {
			if b {
				out = append(out, protocol.MakeIntReply(1))
			} else {
				out = append(out, protocol.MakeIntReply(0))
			}
		}
		return protocol.MakeMultiRawReply(out)
	case "FLUSH":
		server.scriptEngine.Flush()
		return protocol.MakeOkReply()
	case "KILL":
		if err := server.scriptEngine.Kill(); err != nil {
			return protocol.MakeErrReply("NOTBUSY No scripts in execution right now.")
		}
		return protocol.MakeOkReply()
	}
	return protocol.MakeErrReply("ERR Unknown SCRIPT subcommand")
}
