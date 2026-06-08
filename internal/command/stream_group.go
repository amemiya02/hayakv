package database

import (
	"strconv"
	"strings"
	"time"

	"github.com/amemiya02/hayakv/internal/datastruct/stream"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// parseStreamStartID parses the start ID for a consumer group.
// "$" means the last entry ID, "0" means {0, 0}.
func parseStreamStartID(s *stream.Stream, arg string) (stream.StreamID, protocol.ErrorReply) {
	if arg == "$" {
		return s.LastID(), nil
	}
	if arg == "0" {
		return stream.StreamID{Ms: 0, Seq: 0}, nil
	}
	id, err := stream.ParseID(arg)
	if err != nil {
		return stream.StreamID{}, protocol.MakeErrReply("ERR Invalid stream ID specified as stream command argument")
	}
	return id, nil
}

// execXGroup handles XGROUP subcommands.
// XGROUP CREATE key groupname id [MKSTREAM] [ENTRIESADDED count]
// XGROUP SETID key groupname id [ENTRIESADDED count]
// XGROUP DESTROY key groupname
// XGROUP CREATECONSUMER key groupname consumername
// XGROUP DELCONSUMER key groupname consumername
func execXGroup(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'xgroup' command")
	}
	sub := strings.ToUpper(string(args[0]))

	switch sub {
	case "CREATE":
		return execXGroupCreate(db, args[1:])
	case "SETID":
		return execXGroupSetID(db, args[1:])
	case "DESTROY":
		return execXGroupDestroy(db, args[1:])
	case "CREATECONSUMER":
		return execXGroupCreateConsumer(db, args[1:])
	case "DELCONSUMER":
		return execXGroupDelConsumer(db, args[1:])
	default:
		return protocol.MakeErrReply("ERR Unknown subcommand or wrong number of arguments for 'XGROUP' command")
	}
}

// execXGroupCreate implements XGROUP CREATE key groupname id [MKSTREAM] [ENTRIESADDED count]
func execXGroupCreate(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])
	startArg := string(args[2])

	mkstream := false
	entriesAdded := uint64(0)
	rest := args[3:]
	for len(rest) > 0 {
		opt := strings.ToUpper(string(rest[0]))
		switch opt {
		case "MKSTREAM":
			mkstream = true
			rest = rest[1:]
		case "ENTRIESADDED":
			if len(rest) < 2 {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.ParseUint(string(rest[1]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			entriesAdded = n
			rest = rest[2:]
		default:
			return protocol.MakeSyntaxErrReply()
		}
	}

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		if mkstream {
			var created bool
			s, _, errReply = db.getOrInitStream(key)
			if errReply != nil {
				return errReply
			}
			_ = created
		} else {
			return protocol.MakeErrReply("ERR The XGROUP subcommand requires the key to exist")
		}
	}

	startID, errReply := parseStreamStartID(s, startArg)
	if errReply != nil {
		return errReply
	}

	_, err := s.CreateGroup(groupName, startID)
	if err != nil {
		return protocol.MakeErrReply("BUSYGROUP Consumer Group name already exists")
	}

	if entriesAdded > 0 {
		g := s.GetGroup(groupName)
		if g != nil {
			// The group doesn't expose entriesRead directly, but we can set
			// the group's last delivered ID via SetID. We need a different
			// approach for entriesRead. For now, we set the ID and note that
			// entriesRead is handled internally by the group.
			// We can use SetID to adjust the start point.
			// Actually entriesAdded is tracked at the group level for lag
			// calculation. The group stores entriesRead internally.
			// We need to provide the correct lastDelivered so that ReadNew works.
			_ = g
		}
	}

	// Write to AOF
	db.addAof(utils.ToCmdLine3("xgroup", args...))
	return protocol.MakeOkReply()
}

// execXGroupSetID implements XGROUP SETID key groupname id [ENTRIESADDED count]
func execXGroupSetID(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])
	startArg := string(args[2])

	entriesAdded := uint64(0)
	rest := args[3:]
	for len(rest) > 0 {
		opt := strings.ToUpper(string(rest[0]))
		switch opt {
		case "ENTRIESADDED":
			if len(rest) < 2 {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.ParseUint(string(rest[1]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			entriesAdded = n
			rest = rest[2:]
		default:
			return protocol.MakeSyntaxErrReply()
		}
	}

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeErrReply("ERR The XGROUP subcommand requires the key to exist")
	}

	g := s.GetGroup(groupName)
	if g == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	startID, errReply := parseStreamStartID(s, startArg)
	if errReply != nil {
		return errReply
	}

	g.SetID(startID)
	_ = entriesAdded // entriesRead is internal to group

	db.addAof(utils.ToCmdLine3("xgroup", args...))
	return protocol.MakeOkReply()
}

// execXGroupDestroy implements XGROUP DESTROY key groupname
func execXGroupDestroy(db *DB, args [][]byte) redis.Reply {
	if len(args) < 2 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeIntReply(0)
	}

	if s.DestroyGroup(groupName) {
		db.addAof(utils.ToCmdLine3("xgroup", args...))
		return protocol.MakeIntReply(1)
	}
	return protocol.MakeIntReply(0)
}

// execXGroupCreateConsumer implements XGROUP CREATECONSUMER key groupname consumername
func execXGroupCreateConsumer(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])
	consumerName := string(args[2])

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeIntReply(0)
	}

	g := s.GetGroup(groupName)
	if g == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	consumers := g.Consumers()
	if _, exists := consumers[consumerName]; exists {
		return protocol.MakeIntReply(0)
	}

	// Create consumer by reading 0 entries (which triggers getOrCreateConsumer)
	g.ReadNew(s, consumerName, 0)

	db.addAof(utils.ToCmdLine3("xgroup", args...))
	return protocol.MakeIntReply(1)
}

// execXGroupDelConsumer implements XGROUP DELCONSUMER key groupname consumername
func execXGroupDelConsumer(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])
	consumerName := string(args[2])

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeIntReply(0)
	}

	g := s.GetGroup(groupName)
	if g == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	consumers := g.Consumers()
	c, exists := consumers[consumerName]
	if !exists {
		return protocol.MakeIntReply(0)
	}

	pendingCount := int64(len(c.Pending))
	// Ack all pending entries for this consumer
	ids := make([]stream.StreamID, 0, len(c.Pending))
	for id := range c.Pending {
		ids = append(ids, id)
	}
	g.Ack(ids)

	delete(consumers, consumerName)

	db.addAof(utils.ToCmdLine3("xgroup", args...))
	return protocol.MakeIntReply(pendingCount)
}

// execXReadGroup implements XREADGROUP GROUP groupname consumername [COUNT count] [NOACK] [BLOCK milliseconds] STREAMS key [key ...] id [id ...]
func execXReadGroup(db *DB, args [][]byte) redis.Reply {
	if len(args) < 2 {
		return protocol.MakeSyntaxErrReply()
	}

	groupName := ""
	consumerName := ""
	count := 0
	noack := false
	blockMs := int64(-1) // -1 = no block

	i := 0
	for i < len(args) {
		opt := strings.ToUpper(string(args[i]))
		switch opt {
		case "GROUP":
			if i+2 > len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			groupName = string(args[i+1])
			consumerName = string(args[i+2])
			i += 3
		case "COUNT":
			if i+1 >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.Atoi(string(args[i+1]))
			if err != nil || n <= 0 {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			count = n
			i += 2
		case "NOACK":
			noack = true
			i++
		case "BLOCK":
			if i+1 >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			blockMs = n
			i += 2
		case "STREAMS":
			i++
			goto parseStreams
		default:
			return protocol.MakeSyntaxErrReply()
		}
	}
	return protocol.MakeSyntaxErrReply()

parseStreams:
	if groupName == "" || consumerName == "" {
		return protocol.MakeSyntaxErrReply()
	}

	remaining := args[i:]
	if len(remaining) == 0 || len(remaining)%2 != 0 {
		return protocol.MakeSyntaxErrReply()
	}

	numKeys := len(remaining) / 2
	keys := make([]string, numKeys)
	ids := make([]string, numKeys)
	for j := 0; j < numKeys; j++ {
		keys[j] = string(remaining[j])
		ids[j] = string(remaining[numKeys+j])
	}

	// Non-blocking only; blocking will be added in Task 7
	if blockMs >= 0 {
		// For now, treat as non-blocking (0 timeout = immediate return nil)
		if blockMs == 0 {
			return &protocol.NullBulkReply{}
		}
		// Non-zero block: fall through to immediate read for now
	}

	var resultReplies []redis.Reply
	for j, key := range keys {
		s, errReply := db.getAsStream(key)
		if errReply != nil {
			return errReply
		}
		if s == nil {
			continue
		}

		g := s.GetGroup(groupName)
		if g == nil {
			return protocol.MakeErrReply("NOGROUP No such consumer group '" + groupName + "' in key '" + key + "'")
		}

		var entries []stream.Entry
		if ids[j] == ">" {
			entries = g.ReadNew(s, consumerName, count)
		} else {
			// Read from PEL
			startID, err := stream.ParseID(ids[j])
			if err != nil {
				return protocol.MakeErrReply("ERR Invalid stream ID specified as stream command argument")
			}
			endID := stream.StreamID{Ms: ^uint64(0), Seq: ^uint64(0)}
			entries = g.ReadPending(consumerName, startID, endID, count)
			if noack {
				// If NOACK, ack the entries immediately
				ackIDs := make([]stream.StreamID, 0, len(entries))
				for _, e := range entries {
					ackIDs = append(ackIDs, e.ID)
				}
				g.Ack(ackIDs)
			}
		}

		if len(entries) == 0 {
			continue
		}

		// Build stream reply: [stream-key, [[id, [field, value, ...]], ...]]
		entryReplies := make([]redis.Reply, 0, len(entries))
		for _, e := range entries {
			// For pending reads, we need to look up the actual entry data
			fields := e.Fields
			if fields == nil {
				// Look up the entry in the stream to get its fields
				rangeEntries := s.Range(e.ID, e.ID, 1)
				if len(rangeEntries) > 0 {
					fields = rangeEntries[0].Fields
				}
			}

			fieldArgs := make([][]byte, 0, 1+len(fields)*2)
			fieldArgs = append(fieldArgs, []byte(e.ID.String()))
			for _, f := range fields {
				fieldArgs = append(fieldArgs, []byte(f[0]), []byte(f[1]))
			}
			entryReplies = append(entryReplies, protocol.MakeMultiBulkReply(fieldArgs))
		}

		streamReply := make([]redis.Reply, 2)
		streamReply[0] = protocol.MakeBulkReply([]byte(key))
		streamReply[1] = protocol.MakeMultiRawReply(entryReplies)
		resultReplies = append(resultReplies, protocol.MakeMultiRawReply(streamReply))
	}

	if len(resultReplies) == 0 {
		return &protocol.NullBulkReply{}
	}
	return protocol.MakeMultiRawReply(resultReplies)
}

// execXAck implements XACK key groupname id [id ...]
func execXAck(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])

	ids := make([]stream.StreamID, 0, len(args)-2)
	for _, arg := range args[2:] {
		id, err := stream.ParseID(string(arg))
		if err != nil {
			return protocol.MakeErrReply("ERR Invalid stream ID specified as stream command argument")
		}
		ids = append(ids, id)
	}

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeIntReply(0)
	}

	g := s.GetGroup(groupName)
	if g == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	acked := g.Ack(ids)
	db.addAof(utils.ToCmdLine3("xack", args...))
	return protocol.MakeIntReply(int64(acked))
}

// execXPending implements XPENDING key groupname [start end count [consumer]]
func execXPending(db *DB, args [][]byte) redis.Reply {
	if len(args) < 2 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeEmptyMultiBulkReply()
	}

	g := s.GetGroup(groupName)
	if g == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	// Summary form: XPENDING key groupname
	if len(args) == 2 {
		return xpendingSummary(g)
	}

	// Extended form: XPENDING key groupname start end count [consumer]
	if len(args) < 5 {
		return protocol.MakeSyntaxErrReply()
	}

	startArg := string(args[2])
	endArg := string(args[3])
	count, err := strconv.Atoi(string(args[4]))
	if err != nil || count < 0 {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}

	consumerName := ""
	if len(args) >= 6 {
		consumerName = string(args[5])
	}

	startID, err := stream.ParseID(startArg)
	if err != nil {
		return protocol.MakeErrReply("ERR Invalid stream ID specified as stream command argument")
	}
	endID, err := stream.ParseID(endArg)
	if err != nil {
		return protocol.MakeErrReply("ERR Invalid stream ID specified as stream command argument")
	}

	return xpendingExtended(g, s, startID, endID, count, consumerName)
}

// xpendingSummary returns the summary form of XPENDING
func xpendingSummary(g *stream.Group) redis.Reply {
	if g.PendingCount() == 0 {
		return protocol.MakeMultiRawReply([]redis.Reply{
			protocol.MakeIntReply(0),
		})
	}

	consumers := g.Consumers()
	// Find smallest and largest IDs
	smallestID := stream.StreamID{Ms: ^uint64(0), Seq: ^uint64(0)}
	largestID := stream.StreamID{Ms: 0, Seq: 0}
	consumerCounts := make(map[string]int64)

	for _, c := range consumers {
		for id := range c.Pending {
			if id.Ms < smallestID.Ms || (id.Ms == smallestID.Ms && id.Seq < smallestID.Seq) {
				smallestID = id
			}
			if id.Ms > largestID.Ms || (id.Ms == largestID.Ms && id.Seq > largestID.Seq) {
				largestID = id
			}
			consumerCounts[c.Name]++
		}
	}

	// Build consumer info: [consumer-name, count, ...]
	consumerInfo := make([]redis.Reply, 0, len(consumerCounts)*2)
	for name, cnt := range consumerCounts {
		consumerInfo = append(consumerInfo,
			protocol.MakeBulkReply([]byte(name)),
			protocol.MakeIntReply(cnt))
	}

	return protocol.MakeMultiRawReply([]redis.Reply{
		protocol.MakeIntReply(int64(g.PendingCount())),
		protocol.MakeBulkReply([]byte(smallestID.String())),
		protocol.MakeBulkReply([]byte(largestID.String())),
		protocol.MakeMultiRawReply(consumerInfo),
	})
}

// xpendingExtended returns the extended form of XPENDING
func xpendingExtended(g *stream.Group, s *stream.Stream, start, end stream.StreamID, count int, consumerName string) redis.Reply {
	consumers := g.Consumers()
	now := time.Now().UnixMilli()
	var resultReplies []redis.Reply

	for _, c := range consumers {
		if consumerName != "" && c.Name != consumerName {
			continue
		}
		for id, pe := range c.Pending {
			if !id.Greater(start) && !(id.Ms == start.Ms && id.Seq == start.Seq) {
				continue
			}
			if id.Greater(end) {
				continue
			}
			idleTime := now - pe.DeliveryTime
			if idleTime < 0 {
				idleTime = 0
			}
			entry := []redis.Reply{
				protocol.MakeBulkReply([]byte(id.String())),
				protocol.MakeBulkReply([]byte(pe.Consumer)),
				protocol.MakeIntReply(idleTime),
				protocol.MakeIntReply(int64(pe.DeliveryCount)),
			}
			resultReplies = append(resultReplies, protocol.MakeMultiRawReply(entry))
			if count > 0 && len(resultReplies) >= count {
				return protocol.MakeMultiRawReply(resultReplies)
			}
		}
	}

	if resultReplies == nil {
		return protocol.MakeEmptyMultiBulkReply()
	}
	return protocol.MakeMultiRawReply(resultReplies)
}

// execXClaim implements XCLAIM key groupname consumername min-idle-time id [id ...] [IDLE ms] [TIME ms-unix-time] [RETRYCOUNT count] [FORCE] [JUSTID]
func execXClaim(db *DB, args [][]byte) redis.Reply {
	if len(args) < 4 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])
	consumerName := string(args[2])
	minIdleTime, err := strconv.ParseInt(string(args[3]), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}

	var ids []stream.StreamID
	idleMs := int64(-1)
	timeMs := int64(-1)
	retryCount := -1
	force := false
	justid := false

	i := 4
	// Parse IDs first (until we hit an option keyword)
	for i < len(args) {
		arg := strings.ToUpper(string(args[i]))
		if arg == "IDLE" || arg == "TIME" || arg == "RETRYCOUNT" || arg == "FORCE" || arg == "JUSTID" {
			break
		}
		id, err := stream.ParseID(string(args[i]))
		if err != nil {
			return protocol.MakeErrReply("ERR Invalid stream ID specified as stream command argument")
		}
		ids = append(ids, id)
		i++
	}

	// Parse options
	for i < len(args) {
		opt := strings.ToUpper(string(args[i]))
		switch opt {
		case "IDLE":
			if i+1 >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			idleMs = n
			i += 2
		case "TIME":
			if i+1 >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.ParseInt(string(args[i+1]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			timeMs = n
			i += 2
		case "RETRYCOUNT":
			if i+1 >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.Atoi(string(args[i+1]))
			if err != nil {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			retryCount = n
			i += 2
		case "FORCE":
			force = true
			i++
		case "JUSTID":
			justid = true
			i++
		default:
			return protocol.MakeSyntaxErrReply()
		}
	}

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeEmptyMultiBulkReply()
	}

	g := s.GetGroup(groupName)
	if g == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	claimed := g.Claim(consumerName, minIdleTime, ids, force)

	// Adjust delivery time / retry count if specified
	if idleMs >= 0 || timeMs >= 0 || retryCount >= 0 {
		consumers := g.Consumers()
		c, ok := consumers[consumerName]
		if ok {
			for _, id := range claimed {
				pe, ok := c.Pending[id]
				if !ok {
					continue
				}
				if idleMs >= 0 {
					pe.DeliveryTime = time.Now().UnixMilli() - idleMs
				} else if timeMs >= 0 {
					pe.DeliveryTime = timeMs
				}
				if retryCount >= 0 {
					pe.DeliveryCount = uint64(retryCount)
				}
			}
		}
	}

	if justid {
		resultReplies := make([]redis.Reply, 0, len(claimed))
		for _, id := range claimed {
			resultReplies = append(resultReplies, protocol.MakeBulkReply([]byte(id.String())))
		}
		return protocol.MakeMultiRawReply(resultReplies)
	}

	// Return full entries
	resultReplies := make([]redis.Reply, 0, len(claimed))
	for _, id := range claimed {
		rangeEntries := s.Range(id, id, 1)
		var fieldArgs [][]byte
		if len(rangeEntries) > 0 {
			fieldArgs = make([][]byte, 0, 1+len(rangeEntries[0].Fields)*2)
			fieldArgs = append(fieldArgs, []byte(id.String()))
			for _, f := range rangeEntries[0].Fields {
				fieldArgs = append(fieldArgs, []byte(f[0]), []byte(f[1]))
			}
		} else {
			fieldArgs = [][]byte{[]byte(id.String())}
		}
		resultReplies = append(resultReplies, protocol.MakeMultiBulkReply(fieldArgs))
	}

	if len(resultReplies) == 0 {
		return protocol.MakeEmptyMultiBulkReply()
	}
	db.addAof(utils.ToCmdLine3("xclaim", args...))
	return protocol.MakeMultiRawReply(resultReplies)
}

// execXAutoClaim implements XAUTOCLAIM key groupname consumername min-idle-time start [COUNT count] [JUSTID]
func execXAutoClaim(db *DB, args [][]byte) redis.Reply {
	if len(args) < 5 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])
	consumerName := string(args[2])
	minIdleTime, err := strconv.ParseInt(string(args[3]), 10, 64)
	if err != nil {
		return protocol.MakeErrReply("ERR value is not an integer or out of range")
	}
	startArg := string(args[4])

	count := 100 // default
	justid := false

	i := 5
	for i < len(args) {
		opt := strings.ToUpper(string(args[i]))
		switch opt {
		case "COUNT":
			if i+1 >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.Atoi(string(args[i+1]))
			if err != nil || n <= 0 {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			count = n
			i += 2
		case "JUSTID":
			justid = true
			i++
		default:
			return protocol.MakeSyntaxErrReply()
		}
	}

	startID, err := stream.ParseID(startArg)
	if err != nil {
		return protocol.MakeErrReply("ERR Invalid stream ID specified as stream command argument")
	}

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	g := s.GetGroup(groupName)
	if g == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	// Find pending entries that are idle >= minIdleTime and have ID >= startID
	now := time.Now().UnixMilli()
	consumers := g.Consumers()
	var toClaim []stream.StreamID
	nextStart := startID

	for _, c := range consumers {
		for id := range c.Pending {
			if !id.Greater(startID) && !(id.Ms == startID.Ms && id.Seq == startID.Seq) {
				continue
			}
			pe := c.Pending[id]
			if now-pe.DeliveryTime >= minIdleTime {
				toClaim = append(toClaim, id)
			}
		}
	}

	// Sort by ID for consistent ordering (simple bubble sort for small lists)
	for i := 0; i < len(toClaim); i++ {
		for j := i + 1; j < len(toClaim); j++ {
			if toClaim[j].Greater(toClaim[i]) {
				toClaim[i], toClaim[j] = toClaim[j], toClaim[i]
			}
		}
	}

	if len(toClaim) > count {
		toClaim = toClaim[:count]
	}

	// Claim the entries
	claimed := g.Claim(consumerName, minIdleTime, toClaim, false)

	// Determine next start ID
	if len(claimed) > 0 {
		lastClaimed := claimed[len(claimed)-1]
		nextStart = stream.StreamID{Ms: lastClaimed.Ms, Seq: lastClaimed.Seq + 1}
		if nextStart.Seq == 0 { // overflow
			nextStart.Ms++
		}
	} else {
		// No entries claimed, next start is the current start
		nextStart = startID
	}

	// Build reply: [next-start-id, entries, []]
	nextStartReply := protocol.MakeBulkReply([]byte(nextStart.String()))

	if justid {
		idReplies := make([]redis.Reply, 0, len(claimed))
		for _, id := range claimed {
			idReplies = append(idReplies, protocol.MakeBulkReply([]byte(id.String())))
		}
		return protocol.MakeMultiRawReply([]redis.Reply{
			nextStartReply,
			protocol.MakeMultiRawReply(idReplies),
			protocol.MakeEmptyMultiBulkReply(),
		})
	}

	// Full entries
	entryReplies := make([]redis.Reply, 0, len(claimed))
	for _, id := range claimed {
		rangeEntries := s.Range(id, id, 1)
		var fieldArgs [][]byte
		if len(rangeEntries) > 0 {
			fieldArgs = make([][]byte, 0, 1+len(rangeEntries[0].Fields)*2)
			fieldArgs = append(fieldArgs, []byte(id.String()))
			for _, f := range rangeEntries[0].Fields {
				fieldArgs = append(fieldArgs, []byte(f[0]), []byte(f[1]))
			}
		} else {
			fieldArgs = [][]byte{[]byte(id.String())}
		}
		entryReplies = append(entryReplies, protocol.MakeMultiBulkReply(fieldArgs))
	}

	db.addAof(utils.ToCmdLine3("xautoclaim", args...))
	return protocol.MakeMultiRawReply([]redis.Reply{
		nextStartReply,
		protocol.MakeMultiRawReply(entryReplies),
		protocol.MakeEmptyMultiBulkReply(),
	})
}

// execXInfo handles XINFO subcommands.
// XINFO STREAM key [FULL [COUNT count]]
// XINFO GROUPS key
// XINFO CONSUMERS key groupname
func execXInfo(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'xinfo' command")
	}
	sub := strings.ToUpper(string(args[0]))

	switch sub {
	case "STREAM":
		return execXInfoStream(db, args[1:])
	case "GROUPS":
		return execXInfoGroups(db, args[1:])
	case "CONSUMERS":
		return execXInfoConsumers(db, args[1:])
	default:
		return protocol.MakeErrReply("ERR Unknown subcommand or wrong number of arguments for 'XINFO' command")
	}
}

// execXInfoStream implements XINFO STREAM key [FULL [COUNT count]]
func execXInfoStream(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])

	full := false
	count := 10
	rest := args[1:]
	for len(rest) > 0 {
		opt := strings.ToUpper(string(rest[0]))
		switch opt {
		case "FULL":
			full = true
			rest = rest[1:]
		case "COUNT":
			if len(rest) < 2 {
				return protocol.MakeSyntaxErrReply()
			}
			n, err := strconv.Atoi(string(rest[1]))
			if err != nil || n < 0 {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			count = n
			rest = rest[2:]
		default:
			return protocol.MakeSyntaxErrReply()
		}
	}

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeErrReply("ERR no such key")
	}

	groups := s.Groups()
	lastID := s.LastID()
	maxDeletedID := s.MaxDeletedID()
	entriesAdded := s.EntriesAdded()

	// Build reply
	var replies []redis.Reply
	replies = append(replies,
		protocol.MakeBulkReply([]byte("length")),
		protocol.MakeIntReply(int64(s.Len())),
	)
	replies = append(replies,
		protocol.MakeBulkReply([]byte("radix-tree-keys")),
		protocol.MakeIntReply(1), // simplified
	)
	replies = append(replies,
		protocol.MakeBulkReply([]byte("radix-tree-nodes")),
		protocol.MakeIntReply(2), // simplified
	)
	replies = append(replies,
		protocol.MakeBulkReply([]byte("groups")),
		protocol.MakeIntReply(int64(len(groups))),
	)
	replies = append(replies,
		protocol.MakeBulkReply([]byte("last-generated-id")),
		protocol.MakeBulkReply([]byte(lastID.String())),
	)
	replies = append(replies,
		protocol.MakeBulkReply([]byte("max-deleted-entry-id")),
		protocol.MakeBulkReply([]byte(maxDeletedID.String())),
	)
	replies = append(replies,
		protocol.MakeBulkReply([]byte("entries-added")),
		protocol.MakeIntReply(int64(entriesAdded)),
	)

	// First entry
	rangeEntries := s.Range(stream.StreamID{Ms: 0, Seq: 0}, stream.StreamID{Ms: ^uint64(0), Seq: ^uint64(0)}, 1)
	if len(rangeEntries) > 0 {
		e := rangeEntries[0]
		fieldArgs := make([][]byte, 0, 1+len(e.Fields)*2)
		fieldArgs = append(fieldArgs, []byte(e.ID.String()))
		for _, f := range e.Fields {
			fieldArgs = append(fieldArgs, []byte(f[0]), []byte(f[1]))
		}
		replies = append(replies,
			protocol.MakeBulkReply([]byte("first-entry")),
			protocol.MakeMultiBulkReply(fieldArgs),
		)
	} else {
		replies = append(replies,
			protocol.MakeBulkReply([]byte("first-entry")),
			protocol.MakeEmptyMultiBulkReply(),
		)
	}

	// Last entry
	if s.Len() > 0 {
		lastEntries := s.Range(lastID, lastID, 1)
		if len(lastEntries) > 0 {
			e := lastEntries[0]
			fieldArgs := make([][]byte, 0, 1+len(e.Fields)*2)
			fieldArgs = append(fieldArgs, []byte(e.ID.String()))
			for _, f := range e.Fields {
				fieldArgs = append(fieldArgs, []byte(f[0]), []byte(f[1]))
			}
			replies = append(replies,
				protocol.MakeBulkReply([]byte("last-entry")),
				protocol.MakeMultiBulkReply(fieldArgs),
			)
		} else {
			replies = append(replies,
				protocol.MakeBulkReply([]byte("last-entry")),
				protocol.MakeEmptyMultiBulkReply(),
			)
		}
	} else {
		replies = append(replies,
			protocol.MakeBulkReply([]byte("last-entry")),
			protocol.MakeEmptyMultiBulkReply(),
		)
	}

	if full {
		// FULL mode: include group and entry details
		// For now, return basic info; full implementation can be extended
		_ = count
	}

	return protocol.MakeMultiRawReply(replies)
}

// execXInfoGroups implements XINFO GROUPS key
func execXInfoGroups(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeErrReply("ERR no such key")
	}

	groups := s.Groups()
	if len(groups) == 0 {
		return protocol.MakeEmptyMultiBulkReply()
	}

	entriesAdded := s.EntriesAdded()
	var resultReplies []redis.Reply

	for _, g := range groups {
		lastDelivered := g.LastDelivered()
		pendingCount := g.PendingCount()
		consumers := g.Consumers()
		lag := g.Lag(entriesAdded)

		groupReply := []redis.Reply{
			protocol.MakeBulkReply([]byte("name")),
			protocol.MakeBulkReply([]byte(g.Name)),
			protocol.MakeBulkReply([]byte("consumers")),
			protocol.MakeIntReply(int64(len(consumers))),
			protocol.MakeBulkReply([]byte("pending")),
			protocol.MakeIntReply(int64(pendingCount)),
			protocol.MakeBulkReply([]byte("last-delivered-id")),
			protocol.MakeBulkReply([]byte(lastDelivered.String())),
			protocol.MakeBulkReply([]byte("entries-read")),
			protocol.MakeIntReply(int64(entriesAdded)), // simplified
			protocol.MakeBulkReply([]byte("lag")),
			protocol.MakeIntReply(int64(lag)),
		}
		resultReplies = append(resultReplies, protocol.MakeMultiRawReply(groupReply))
	}

	return protocol.MakeMultiRawReply(resultReplies)
}

// execXInfoConsumers implements XINFO CONSUMERS key groupname
func execXInfoConsumers(db *DB, args [][]byte) redis.Reply {
	if len(args) < 2 {
		return protocol.MakeSyntaxErrReply()
	}
	key := string(args[0])
	groupName := string(args[1])

	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeErrReply("ERR no such key")
	}

	g := s.GetGroup(groupName)
	if g == nil {
		return protocol.MakeErrReply("NOGROUP No such consumer group")
	}

	consumers := g.Consumers()
	if len(consumers) == 0 {
		return protocol.MakeEmptyMultiBulkReply()
	}

	now := time.Now().UnixMilli()
	var resultReplies []redis.Reply

	for _, c := range consumers {
		pendingCount := int64(len(c.Pending))
		idleTime := now - c.ActiveTime
		if idleTime < 0 {
			idleTime = 0
		}

		consumerReply := []redis.Reply{
			protocol.MakeBulkReply([]byte("name")),
			protocol.MakeBulkReply([]byte(c.Name)),
			protocol.MakeBulkReply([]byte("pending")),
			protocol.MakeIntReply(pendingCount),
			protocol.MakeBulkReply([]byte("idle")),
			protocol.MakeIntReply(idleTime),
		}
		resultReplies = append(resultReplies, protocol.MakeMultiRawReply(consumerReply))
	}

	return protocol.MakeMultiRawReply(resultReplies)
}

func init() {
	registerCommand("XGroup", execXGroup, writeFirstKey, nil, -4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM}, 1, 1, 1).
		attachNotify(notifyStream, "xgroup-create")
	registerCommand("XReadGroup", execXReadGroup, readFirstKey, nil, -7, flagWrite).
		attachCommandExtra([]string{redisFlagWrite}, 1, 1, 1)
	registerCommand("XAck", execXAck, writeFirstKey, nil, -4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1)
	registerCommand("XPending", execXPending, readFirstKey, nil, -3, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 1, 1, 1)
	registerCommand("XClaim", execXClaim, writeFirstKey, nil, -6, flagWrite).
		attachCommandExtra([]string{redisFlagWrite}, 1, 1, 1)
	registerCommand("XAutoClaim", execXAutoClaim, writeFirstKey, nil, -5, flagWrite).
		attachCommandExtra([]string{redisFlagWrite}, 1, 1, 1)
	registerCommand("XInfo", execXInfo, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 1, 1, 1)
}
