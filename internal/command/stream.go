package database

import (
	"strconv"
	"strings"
	"time"

	"github.com/amemiya02/hayakv/internal/datastruct/stream"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// getAsStream returns the Stream object for the given key.
// Returns WrongTypeErrReply if the key is not a stream type.
func (db *DB) getAsStream(key string) (*stream.Stream, protocol.ErrorReply) {
	entity, exists := db.GetEntity(key)
	if !exists {
		return nil, nil
	}
	robj, ok := entity.Data.(*object.Robj)
	if !ok {
		return nil, &protocol.WrongTypeErrReply{}
	}
	if robj.Type != object.TypeStream {
		return nil, &protocol.WrongTypeErrReply{}
	}
	return robj.Value().(*stream.Stream), nil
}

// getOrInitStream returns the Stream object for the given key, creating it if it does not exist.
// Returns inited=true if a new stream was created.
func (db *DB) getOrInitStream(key string) (s *stream.Stream, inited bool, errReply protocol.ErrorReply) {
	entity, exists := db.GetEntity(key)
	if exists {
		robj, ok := entity.Data.(*object.Robj)
		if !ok {
			return nil, false, &protocol.WrongTypeErrReply{}
		}
		if robj.Type != object.TypeStream {
			return nil, false, &protocol.WrongTypeErrReply{}
		}
		return robj.Value().(*stream.Stream), false, nil
	}
	s = stream.New()
	robj := &object.Robj{
		Type:     object.TypeStream,
		Encoding: object.EncStream,
		Ptr:      s,
	}
	db.PutEntity(key, &database.DataEntity{
		Data: robj,
	})
	return s, true, nil
}

// execXAdd implements XADD key [NOMKSTREAM] [MAXLEN [=|~] threshold [LIMIT n]] [MINID [=|~] threshold [LIMIT n]] <*|id> field value [field value ...]
func execXAdd(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeArgNumErrReply("xadd")
	}

	key := string(args[0])
	idx := 1

	// Parse NOMKSTREAM
	noMkStream := false
	if idx < len(args) && strings.ToUpper(string(args[idx])) == "NOMKSTREAM" {
		noMkStream = true
		idx++
	}

	// Parse MAXLEN/MINID trimming options
	var trimMaxLen int64 = -1
	var trimMinID stream.StreamID
	var trimMaxLenSet bool
	var trimMinIDSet bool
	var trimApprox bool
	var trimLimit int64 = 0

	for idx < len(args) {
		arg := strings.ToUpper(string(args[idx]))
		if arg == "MAXLEN" {
			idx++
			if idx >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			// Parse = or ~
			next := string(args[idx])
			if next == "=" || next == "~" {
				trimApprox = next == "~"
				idx++
			}
			if idx >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			val, err := strconv.ParseInt(string(args[idx]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR invalid MAXLEN argument")
			}
			if val < 0 {
				return protocol.MakeErrReply("ERR value is negative or out of range")
			}
			trimMaxLen = val
			trimMaxLenSet = true
			idx++
		} else if arg == "MINID" {
			idx++
			if idx >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			// Parse = or ~
			next := string(args[idx])
			if next == "=" || next == "~" {
				trimApprox = next == "~"
				idx++
			}
			if idx >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			id, _, err := stream.ParseID(string(args[idx]))
			if err != nil {
				return protocol.MakeErrReply(err.Error())
			}
			trimMinID = id
			trimMinIDSet = true
			idx++
		} else if arg == "LIMIT" {
			idx++
			if idx >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			val, err := strconv.ParseInt(string(args[idx]), 10, 64)
			if err != nil || val < 0 {
				return protocol.MakeErrReply("ERR invalid LIMIT argument")
			}
			trimLimit = val
			idx++
		} else {
			break
		}
	}

	// Use trimLimit for approx trimming (not used in simplified impl)
	_ = trimLimit

	// Parse ID (must be present now)
	if idx >= len(args) {
		return protocol.MakeSyntaxErrReply()
	}

	idStr := string(args[idx])
	idx++
	autoAssign := idStr == "*"

	var id stream.StreamID
	var autoSeq bool
	if !autoAssign {
		var err error
		id, autoSeq, err = stream.ParseID(idStr)
		if err != nil {
			return protocol.MakeErrReply(err.Error())
		}
		autoAssign = autoSeq // ms-* also auto-assigns
	}

	// Parse field-value pairs
	if idx >= len(args) || (len(args)-idx)%2 != 0 {
		return protocol.MakeSyntaxErrReply()
	}

	fields := make([][2]string, 0, (len(args)-idx)/2)
	for i := idx; i < len(args); i += 2 {
		fields = append(fields, [2]string{string(args[i]), string(args[i+1])})
	}

	// Get or create stream
	if noMkStream {
		s, errReply := db.getAsStream(key)
		if errReply != nil {
			return errReply
		}
		if s == nil {
			return &protocol.NullBulkReply{}
		}
		var assignedID stream.StreamID
		var err error
		if autoAssign {
			msHint := id.Ms // 0 for "*", ms value for "ms-*"
			if msHint == 0 {
				msHint = uint64(time.Now().UnixMilli())
			}
			assignedID, err = s.AddAutoAssign(msHint, fields)
		} else {
			assignedID, err = s.Add(id, fields)
		}
		if err != nil {
			return protocol.MakeErrReply(err.Error())
		}
		applyStreamTrim(s, trimMaxLenSet, trimMaxLen, trimMinIDSet, trimMinID, trimApprox)
		db.addAof(utils.ToCmdLine3("xadd", buildXAddAofArgs(key, assignedID, fields)...))
		return protocol.MakeBulkReply([]byte(assignedID.String()))
	}

	s, _, errReply := db.getOrInitStream(key)
	if errReply != nil {
		return errReply
	}

	// Auto-assign ID
	var assignedID stream.StreamID
	var err error
	if autoAssign {
		msHint := id.Ms // 0 for "*", ms value for "ms-*"
		if msHint == 0 {
			msHint = uint64(time.Now().UnixMilli())
		}
		assignedID, err = s.AddAutoAssign(msHint, fields)
	} else {
		assignedID, err = s.Add(id, fields)
	}
	if err != nil {
		return protocol.MakeErrReply(err.Error())
	}

	applyStreamTrim(s, trimMaxLenSet, trimMaxLen, trimMinIDSet, trimMinID, trimApprox)

	// AOF: rewrite with resolved ID
	db.addAof(utils.ToCmdLine3("xadd", buildXAddAofArgs(key, assignedID, fields)...))
	return protocol.MakeBulkReply([]byte(assignedID.String()))
}

func applyStreamTrim(s *stream.Stream, maxlenSet bool, maxlen int64, minidSet bool, minid stream.StreamID, approx bool) {
	if maxlenSet {
		s.TrimMaxLen(maxlen, approx)
	}
	if minidSet {
		s.TrimMinID(minid, approx)
	}
}

func buildXAddAofArgs(key string, id stream.StreamID, fields [][2]string) [][]byte {
	args := [][]byte{[]byte(key), []byte(id.String())}
	for _, f := range fields {
		args = append(args, []byte(f[0]), []byte(f[1]))
	}
	return args
}

// execXRange implements XRANGE key start end [COUNT count]
func execXRange(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeArgNumErrReply("xrange")
	}

	key := string(args[0])
	startStr := string(args[1])
	endStr := string(args[2])

	// Parse start
	start, _, err := stream.ParseID(startStr)
	if err != nil {
		return protocol.MakeErrReply(err.Error())
	}

	// Parse end
	end, _, err := stream.ParseID(endStr)
	if err != nil {
		return protocol.MakeErrReply(err.Error())
	}

	// Parse optional COUNT
	count := 0
	if len(args) >= 5 {
		if strings.ToUpper(string(args[3])) != "COUNT" {
			return protocol.MakeSyntaxErrReply()
		}
		c, err := strconv.Atoi(string(args[4]))
		if err != nil || c < 0 {
			return protocol.MakeErrReply("ERR value is not an integer or out of range")
		}
		count = c
	}

	// Get stream
	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeMultiRawReply([]redis.Reply{})
	}

	entries := s.Range(start, end, count)
	return streamEntriesToReply(entries)
}

// execXRevRange implements XREVRANGE key end start [COUNT count]
func execXRevRange(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeArgNumErrReply("xrevrange")
	}

	key := string(args[0])
	endStr := string(args[1])
	startStr := string(args[2])

	// Parse start
	start, _, err := stream.ParseID(startStr)
	if err != nil {
		return protocol.MakeErrReply(err.Error())
	}

	// Parse end
	end, _, err := stream.ParseID(endStr)
	if err != nil {
		return protocol.MakeErrReply(err.Error())
	}

	// Parse optional COUNT
	count := 0
	if len(args) >= 5 {
		if strings.ToUpper(string(args[3])) != "COUNT" {
			return protocol.MakeSyntaxErrReply()
		}
		c, err := strconv.Atoi(string(args[4]))
		if err != nil || c < 0 {
			return protocol.MakeErrReply("ERR value is not an integer or out of range")
		}
		count = c
	}

	// Get stream
	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeMultiRawReply([]redis.Reply{})
	}

	entries := s.RevRange(start, end, count)
	return streamEntriesToReply(entries)
}

// streamEntriesToReply converts stream entries to RESP array format
func streamEntriesToReply(entries []stream.Entry) redis.Reply {
	result := make([]redis.Reply, len(entries))
	for i, entry := range entries {
		// Each entry is [id, [field, value, ...]]
		fieldReplies := make([]redis.Reply, 0, len(entry.Fields)*2)
		for _, fv := range entry.Fields {
			fieldReplies = append(fieldReplies,
				protocol.MakeBulkReply([]byte(fv[0])),
				protocol.MakeBulkReply([]byte(fv[1])),
			)
		}
		entryReplies := []redis.Reply{
			protocol.MakeBulkReply([]byte(entry.ID.String())),
			protocol.MakeMultiRawReply(fieldReplies),
		}
		result[i] = protocol.MakeMultiRawReply(entryReplies)
	}
	return protocol.MakeMultiRawReply(result)
}

// execXLen implements XLEN key
func execXLen(db *DB, args [][]byte) redis.Reply {
	key := string(args[0])
	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeIntReply(0)
	}
	return protocol.MakeIntReply(int64(s.Len()))
}

// execXDel implements XDEL key id [id ...]
func execXDel(db *DB, args [][]byte) redis.Reply {
	if len(args) < 2 {
		return protocol.MakeArgNumErrReply("xdel")
	}

	key := string(args[0])
	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeIntReply(0)
	}

	ids := make([]stream.StreamID, 0, len(args)-1)
	for _, arg := range args[1:] {
		id, _, err := stream.ParseID(string(arg))
		if err != nil {
			return protocol.MakeErrReply(err.Error())
		}
		ids = append(ids, id)
	}

	deleted := s.Delete(ids)
	db.addAof(utils.ToCmdLine3("xdel", args...))
	return protocol.MakeIntReply(int64(deleted))
}

// execXTrim implements XTRIM key MAXLEN [=|~] threshold | MINID [=|~] threshold
func execXTrim(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeArgNumErrReply("xtrim")
	}

	key := string(args[0])
	s, errReply := db.getAsStream(key)
	if errReply != nil {
		return errReply
	}
	if s == nil {
		return protocol.MakeIntReply(0)
	}

	arg := strings.ToUpper(string(args[1]))
	if arg == "MAXLEN" {
		// Parse = or ~
		idx := 2
		approx := false
		if idx < len(args) {
			next := string(args[idx])
			if next == "=" || next == "~" {
				approx = next == "~"
				idx++
			}
		}
		if idx >= len(args) {
			return protocol.MakeSyntaxErrReply()
		}
		val, err := strconv.ParseInt(string(args[idx]), 10, 64)
		if err != nil {
			return protocol.MakeErrReply("ERR invalid MAXLEN argument")
		}
		if val < 0 {
			return protocol.MakeErrReply("ERR value is negative or out of range")
		}
		removed := s.TrimMaxLen(val, approx)
		db.addAof(utils.ToCmdLine3("xtrim", args...))
		return protocol.MakeIntReply(removed)
	} else if arg == "MINID" {
		idx := 2
		approx := false
		if idx < len(args) {
			next := string(args[idx])
			if next == "=" || next == "~" {
				approx = next == "~"
				idx++
			}
		}
		if idx >= len(args) {
			return protocol.MakeSyntaxErrReply()
		}
		id, _, err := stream.ParseID(string(args[idx]))
		if err != nil {
			return protocol.MakeErrReply(err.Error())
		}
		removed := s.TrimMinID(id, approx)
		db.addAof(utils.ToCmdLine3("xtrim", args...))
		return protocol.MakeIntReply(removed)
	}

	return protocol.MakeErrReply("ERR syntax error")
}

// execXSetID implements XSETID key last-id [ENTRIESADDED entries-added] [MAXDELETEDID max-deleted-id]
func execXSetID(db *DB, args [][]byte) redis.Reply {
	if len(args) < 2 {
		return protocol.MakeArgNumErrReply("xsetid")
	}

	key := string(args[0])
	lastID, _, err := stream.ParseID(string(args[1]))
	if err != nil {
		return protocol.MakeErrReply(err.Error())
	}

	var entriesAdded uint64
	var maxDeletedID stream.StreamID

	idx := 2
	for idx < len(args) {
		arg := strings.ToUpper(string(args[idx]))
		if arg == "ENTRIESADDED" {
			idx++
			if idx >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			val, err := strconv.ParseUint(string(args[idx]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR value is not an integer or out of range")
			}
			entriesAdded = val
			idx++
		} else if arg == "MAXDELETEDID" {
			idx++
			if idx >= len(args) {
				return protocol.MakeSyntaxErrReply()
			}
			id, _, err := stream.ParseID(string(args[idx]))
			if err != nil {
				return protocol.MakeErrReply(err.Error())
			}
			maxDeletedID = id
			idx++
		} else {
			return protocol.MakeSyntaxErrReply()
		}
	}

	s, _, errReply := db.getOrInitStream(key)
	if errReply != nil {
		return errReply
	}

	s.SetID(lastID, entriesAdded, maxDeletedID)
	db.addAof(utils.ToCmdLine3("xsetid", args...))
	return protocol.MakeStatusReply("OK")
}

// execXRead implements XREAD [COUNT count] [BLOCK milliseconds] STREAMS key [key ...] id [id ...]
// Non-blocking only for now
func execXRead(db *DB, args [][]byte) redis.Reply {
	if len(args) < 4 {
		return protocol.MakeArgNumErrReply("xread")
	}

	idx := 0
	count := 0

	// Parse optional COUNT
	if idx < len(args) && strings.ToUpper(string(args[idx])) == "COUNT" {
		idx++
		if idx >= len(args) {
			return protocol.MakeSyntaxErrReply()
		}
		c, err := strconv.Atoi(string(args[idx]))
		if err != nil || c < 0 {
			return protocol.MakeErrReply("ERR value is not an integer or out of range")
		}
		count = c
		idx++
	}

	// Parse optional BLOCK (non-blocking for now, just consume the arg)
	if idx < len(args) && strings.ToUpper(string(args[idx])) == "BLOCK" {
		idx++
		if idx >= len(args) {
			return protocol.MakeSyntaxErrReply()
		}
		// For non-blocking, just parse and ignore
		_, err := strconv.Atoi(string(args[idx]))
		if err != nil {
			return protocol.MakeErrReply("ERR value is not an integer or out of range")
		}
		idx++
	}

	// Parse STREAMS keyword
	if idx >= len(args) || strings.ToUpper(string(args[idx])) != "STREAMS" {
		return protocol.MakeSyntaxErrReply()
	}
	idx++

	// Remaining args: keys and IDs (paired)
	remaining := len(args) - idx
	if remaining < 2 || remaining%2 != 0 {
		return protocol.MakeErrReply("ERR Unbalanced XREAD list of streams: for each stream key an ID or '$' must be specified")
	}

	numKeys := remaining / 2
	keys := make([]string, numKeys)
	ids := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = string(args[idx+i])
		ids[i] = string(args[idx+numKeys+i])
	}

	// Process each stream
	results := make([]redis.Reply, 0, numKeys)
	for i, key := range keys {
		s, errReply := db.getAsStream(key)
		if errReply != nil {
			return errReply
		}
		if s == nil {
			continue
		}

		// Parse ID: "$" means last ID, otherwise parse
		var startID stream.StreamID
		if ids[i] == "$" {
			startID = s.LastID()
		} else {
			var err error
			startID, _, err = stream.ParseID(ids[i])
			if err != nil {
				return protocol.MakeErrReply(err.Error())
			}
		}

		// Read entries with ID > startID
		// We use Range with start = startID+1
		rangeStart := stream.StreamID{Ms: startID.Ms, Seq: startID.Seq + 1}
		if startID.Seq == ^uint64(0) {
			rangeStart = stream.StreamID{Ms: startID.Ms + 1, Seq: 0}
		}
		end := stream.StreamID{Ms: ^uint64(0), Seq: ^uint64(0)}
		entries := s.Range(rangeStart, end, count)
		if len(entries) == 0 {
			continue
		}

		// Format: [stream-key, [[id, [field, value, ...]], ...]]
		entryReplies := make([]redis.Reply, len(entries))
		for j, entry := range entries {
			fieldReplies := make([]redis.Reply, 0, len(entry.Fields)*2)
			for _, fv := range entry.Fields {
				fieldReplies = append(fieldReplies,
					protocol.MakeBulkReply([]byte(fv[0])),
					protocol.MakeBulkReply([]byte(fv[1])),
				)
			}
			entryReplies[j] = protocol.MakeMultiRawReply([]redis.Reply{
				protocol.MakeBulkReply([]byte(entry.ID.String())),
				protocol.MakeMultiRawReply(fieldReplies),
			})
		}

		streamResult := protocol.MakeMultiRawReply([]redis.Reply{
			protocol.MakeBulkReply([]byte(key)),
			protocol.MakeMultiRawReply(entryReplies),
		})
		results = append(results, streamResult)
	}

	if len(results) == 0 {
		return &protocol.NullMultiBulkReply{}
	}

	return protocol.MakeMultiRawReply(results)
}

func init() {
	registerCommand("XAdd", execXAdd, writeFirstKey, nil, -5, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyStream, "xadd")
	registerCommand("XRange", execXRange, readFirstKey, nil, -4, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 1, 1, 1)
	registerCommand("XRevRange", execXRevRange, readFirstKey, nil, -4, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 1, 1, 1)
	registerCommand("XLen", execXLen, readFirstKey, nil, 2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly, redisFlagFast}, 1, 1, 1)
	registerCommand("XDel", execXDel, writeFirstKey, nil, -3, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagFast}, 1, 1, 1).
		attachNotify(notifyStream, "xdel")
	registerCommand("XTrim", execXTrim, writeFirstKey, nil, -2, flagWrite).
		attachCommandExtra([]string{redisFlagWrite}, 1, 1, 1).
		attachNotify(notifyStream, "xtrim")
	registerCommand("XSetID", execXSetID, writeFirstKey, nil, -3, flagWrite).
		attachCommandExtra([]string{redisFlagWrite}, 1, 1, 1)
	registerCommand("XRead", execXRead, readFirstKey, nil, -4, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 1, 1, 1)
}
