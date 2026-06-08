package database

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

type latencyEvent struct {
	timestamp int64 // unix seconds
	lastMs    int64
	maxMs     int64
}

type latencyMonitor struct {
	mu     sync.Mutex
	events map[string][]latencyEvent // event name -> ring of events
	maxPer int                       // max events per event name (default 160)
}

func newLatencyMonitor() *latencyMonitor {
	return &latencyMonitor{
		events: make(map[string][]latencyEvent),
		maxPer: 160,
	}
}

func (m *latencyMonitor) record(event string, ms int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ring := m.events[event]
	entry := latencyEvent{
		timestamp: time.Now().Unix(),
		lastMs:    ms,
		maxMs:     ms,
	}
	// Update max if previous entries exist
	if len(ring) > 0 && ring[len(ring)-1].maxMs > ms {
		entry.maxMs = ring[len(ring)-1].maxMs
	}
	ring = append(ring, entry)
	if len(ring) > m.maxPer {
		ring = ring[len(ring)-m.maxPer:]
	}
	m.events[event] = ring
}

func (m *latencyMonitor) latest() []latencyEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []latencyEvent
	for _, ring := range m.events {
		if len(ring) > 0 {
			result = append(result, ring[len(ring)-1])
		}
	}
	return result
}

func (m *latencyMonitor) history(event string) []latencyEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events[event]
}

func (m *latencyMonitor) reset(events ...string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(events) == 0 {
		count := len(m.events)
		m.events = make(map[string][]latencyEvent)
		return count
	}
	count := 0
	for _, e := range events {
		if _, ok := m.events[e]; ok {
			delete(m.events, e)
			count++
		}
	}
	return count
}

func execLatency(server *Server, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'latency' command")
	}
	subCmd := strings.ToLower(string(args[0]))
	switch subCmd {
	case "latest":
		return latencyLatest(server)
	case "history":
		return latencyHistory(server, args[1:])
	case "reset":
		return latencyReset(server, args[1:])
	case "doctor":
		return latencyDoctor(server)
	case "graph":
		return latencyGraph(server, args[1:])
	case "help":
		return latencyHelp()
	default:
		return protocol.MakeErrReply("ERR unknown subcommand '" + string(args[0]) + "'")
	}
}

func latencyLatest(server *Server) redis.Reply {
	events := server.latencyMon.latest()
	result := make([]redis.Reply, len(events))
	for i, e := range events {
		result[i] = protocol.MakeMultiRawReply([]redis.Reply{
			protocol.MakeBulkReply([]byte("command")),
			protocol.MakeIntReply(e.timestamp),
			protocol.MakeIntReply(e.lastMs),
			protocol.MakeIntReply(e.maxMs),
		})
	}
	return protocol.MakeMultiRawReply(result)
}

func latencyHistory(server *Server, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'latency|history' command")
	}
	event := string(args[0])
	events := server.latencyMon.history(event)
	result := make([]redis.Reply, len(events))
	for i, e := range events {
		result[i] = protocol.MakeMultiRawReply([]redis.Reply{
			protocol.MakeIntReply(e.timestamp),
			protocol.MakeIntReply(e.lastMs),
		})
	}
	return protocol.MakeMultiRawReply(result)
}

func latencyReset(server *Server, args [][]byte) redis.Reply {
	events := make([]string, len(args))
	for i, a := range args {
		events[i] = string(a)
	}
	count := server.latencyMon.reset(events...)
	return protocol.MakeIntReply(int64(count))
}

func latencyDoctor(server *Server) redis.Reply {
	// Return a simple advisory string
	var b strings.Builder
	b.WriteString("Dave, I have observed latency spikes in this Redis instance.\r\n")
	b.WriteString("This is not necessarily a reason to worry, but here are some tips:\r\n")
	b.WriteString("- Check if you have slow commands: use SLOWLOG GET\r\n")
	b.WriteString("- Check if maxmemory is set and if eviction is happening\r\n")
	return protocol.MakeBulkReply([]byte(b.String()))
}

func latencyGraph(server *Server, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'latency|graph' command")
	}
	event := string(args[0])
	events := server.latencyMon.history(event)
	if len(events) == 0 {
		return protocol.MakeBulkReply([]byte("(nil)"))
	}
	// Simple ASCII graph
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Latency %s graph (max: %d ms):\n", event, events[len(events)-1].maxMs))
	for _, e := range events {
		bar := strings.Repeat("#", int(e.lastMs))
		b.WriteString(fmt.Sprintf("%d %s\n", e.timestamp, bar))
	}
	return protocol.MakeBulkReply([]byte(b.String()))
}

func latencyHelp() redis.Reply {
	return protocol.MakeMultiBulkReply([][]byte{
		[]byte("LATENCY <subcommand> [<arg> [value] ...]"),
		[]byte("LATENCY DOCTOR"),
		[]byte("    Return a human readable latency analysis report."),
		[]byte("LATENCY GRAPH <event>"),
		[]byte("    Return an ASCII latency graph for an event."),
		[]byte("LATENCY HISTORY <event>"),
		[]byte("    Return latency events for an event."),
		[]byte("LATENCY LATEST"),
		[]byte("    Return the latest latency events for all events."),
		[]byte("LATENCY RESET [<event> ...]"),
		[]byte("    Reset latency data for one or more events."),
	})
}
