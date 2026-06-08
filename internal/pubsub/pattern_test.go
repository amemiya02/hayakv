package pubsub

import (
	"strings"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// ---------- helpers ----------

// fakeConn wraps a simple in-memory buffer for testing pubsub delivery.
type fakeConn struct {
	buf   []byte
	subs  map[string]bool
	pSubs map[string]bool
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		subs:  make(map[string]bool),
		pSubs: make(map[string]bool),
	}
}

func (f *fakeConn) Write(b []byte) (int, error) {
	f.buf = append(f.buf, b...)
	return len(b), nil
}

func (f *fakeConn) Close() error          { return nil }
func (f *fakeConn) RemoteAddr() string    { return "fake" }
func (f *fakeConn) SetPassword(string)    {}
func (f *fakeConn) GetPassword() string   { return "" }
func (f *fakeConn) Subscribe(ch string)   { f.subs[ch] = true }
func (f *fakeConn) UnSubscribe(ch string) { delete(f.subs, ch) }
func (f *fakeConn) SubsCount() int        { return len(f.subs) }
func (f *fakeConn) GetChannels() []string {
	out := make([]string, 0, len(f.subs))
	for ch := range f.subs {
		out = append(out, ch)
	}
	return out
}
func (f *fakeConn) PSubscribe(p string)   { f.pSubs[p] = true }
func (f *fakeConn) PUnSubscribe(p string) { delete(f.pSubs, p) }
func (f *fakeConn) PatternCount() int     { return len(f.pSubs) }
func (f *fakeConn) GetPatterns() []string {
	out := make([]string, 0, len(f.pSubs))
	for p := range f.pSubs {
		out = append(out, p)
	}
	return out
}
func (f *fakeConn) InMultiState() bool             { return false }
func (f *fakeConn) SetMultiState(bool)             {}
func (f *fakeConn) GetQueuedCmdLine() [][][]byte   { return nil }
func (f *fakeConn) EnqueueCmd([][]byte)            {}
func (f *fakeConn) ClearQueuedCmds()               {}
func (f *fakeConn) GetWatching() map[string]uint32 { return nil }
func (f *fakeConn) AddTxError(error)               {}
func (f *fakeConn) GetTxErrors() []error           { return nil }
func (f *fakeConn) GetDBIndex() int                { return 0 }
func (f *fakeConn) SelectDB(int)                   {}
func (f *fakeConn) Protocol() redis.RespVersion    { return redis.RESP2 }
func (f *fakeConn) SetProtocol(redis.RespVersion)  {}
func (f *fakeConn) SetSlave()                      {}
func (f *fakeConn) IsSlave() bool                  { return false }
func (f *fakeConn) SetMaster()                     {}
func (f *fakeConn) IsMaster() bool                 { return false }
func (f *fakeConn) Name() string                   { return "fake" }

// client identification stubs
func (f *fakeConn) ClientID() uint64     { return 0 }
func (f *fakeConn) ClientName() string   { return "" }
func (f *fakeConn) SetClientName(string) {}
func (f *fakeConn) LibName() string      { return "" }
func (f *fakeConn) SetLibName(string)    {}
func (f *fakeConn) LibVer() string       { return "" }
func (f *fakeConn) SetLibVer(string)     {}
func (f *fakeConn) CreatedAt() time.Time { return time.Time{} }
func (f *fakeConn) ReplyMode() int       { return 0 }
func (f *fakeConn) SetReplyMode(int)     {}
func (f *fakeConn) IsTracking() bool     { return false }
func (f *fakeConn) SetTracking(bool)     {}
func (f *fakeConn) TrackingMode() int    { return 0 }
func (f *fakeConn) SetTrackingMode(int)  {}
func (f *fakeConn) NoLoop() bool         { return false }
func (f *fakeConn) SetNoLoop(bool)       {}
func (f *fakeConn) RedirectID() uint64   { return 0 }
func (f *fakeConn) SetRedirectID(uint64) {}

// ---------- tests ----------

func TestPSubscribe(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	reply := PSubscribe(hub, c, [][]byte{[]byte("h?llo"), []byte("w*")})
	if _, ok := reply.(*protocol.NoReply); !ok {
		t.Fatalf("expected NoReply, got %T", reply)
	}
	if c.PatternCount() != 2 {
		t.Fatalf("expected 2 patterns, got %d", c.PatternCount())
	}
	if hub.patterns.Len() != 2 {
		t.Fatalf("expected hub.patterns len 2, got %d", hub.patterns.Len())
	}
}

func TestPUnsubscribe(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	PSubscribe(hub, c, [][]byte{[]byte("h?llo"), []byte("w*")})
	reply := PUnsubscribe(hub, c, [][]byte{[]byte("h?llo")})
	if _, ok := reply.(*protocol.NoReply); !ok {
		t.Fatalf("expected NoReply, got %T", reply)
	}
	if c.PatternCount() != 1 {
		t.Fatalf("expected 1 pattern after unsub, got %d", c.PatternCount())
	}
}

func TestPUnsubscribeAll(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	PSubscribe(hub, c, [][]byte{[]byte("a*"), []byte("b*"), []byte("c*")})
	PUnsubscribeAll(hub, c)
	if c.PatternCount() != 0 {
		t.Fatalf("expected 0 patterns after unsub all, got %d", c.PatternCount())
	}
	if hub.patterns.Len() != 0 {
		t.Fatalf("expected hub.patterns len 0 after unsub all, got %d", hub.patterns.Len())
	}
}

func TestPUnsubscribeNothing(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	reply := PUnsubscribe(hub, c, nil)
	if _, ok := reply.(*protocol.NoReply); !ok {
		t.Fatalf("expected NoReply, got %T", reply)
	}
	if !strings.Contains(string(c.buf), "punsubscribe") {
		t.Fatalf("expected punsubscribe message in buffer, got %q", string(c.buf))
	}
}

func TestPublishToPatterns(t *testing.T) {
	hub := MakeHub()
	c1 := newFakeConn()
	c2 := newFakeConn()

	PSubscribe(hub, c1, [][]byte{[]byte("news.*")})
	PSubscribe(hub, c2, [][]byte{[]byte("news.*")})

	reply := Publish(hub, [][]byte{[]byte("news.sports"), []byte("goal!")})
	intReply, ok := reply.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", reply)
	}
	if intReply.Code != 2 {
		t.Fatalf("expected 2 receivers (pattern), got %d", intReply.Code)
	}

	if !strings.Contains(string(c1.buf), "pmessage") {
		t.Fatalf("expected pmessage in c1 buffer, got %q", string(c1.buf))
	}
	if !strings.Contains(string(c1.buf), "news.*") {
		t.Fatalf("expected pattern 'news.*' in c1 buffer, got %q", string(c1.buf))
	}
	if !strings.Contains(string(c1.buf), "news.sports") {
		t.Fatalf("expected channel 'news.sports' in c1 buffer, got %q", string(c1.buf))
	}
	if !strings.Contains(string(c1.buf), "goal!") {
		t.Fatalf("expected message 'goal!' in c1 buffer, got %q", string(c1.buf))
	}
}

func TestPublishCombinedExactAndPattern(t *testing.T) {
	hub := MakeHub()
	c1 := newFakeConn() // exact subscriber
	c2 := newFakeConn() // pattern subscriber

	Subscribe(hub, c1, [][]byte{[]byte("chan")})
	PSubscribe(hub, c2, [][]byte{[]byte("chan*")})

	reply := Publish(hub, [][]byte{[]byte("chan"), []byte("hello")})
	intReply, ok := reply.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", reply)
	}
	if intReply.Code != 2 {
		t.Fatalf("expected 2 receivers (1 exact + 1 pattern), got %d", intReply.Code)
	}

	if !strings.Contains(string(c1.buf), "message") {
		t.Fatalf("expected 'message' in c1 buffer, got %q", string(c1.buf))
	}
	if !strings.Contains(string(c2.buf), "pmessage") {
		t.Fatalf("expected 'pmessage' in c2 buffer, got %q", string(c2.buf))
	}
}

func TestPublishNoPatternMatch(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	PSubscribe(hub, c, [][]byte{[]byte("news.*")})

	reply := Publish(hub, [][]byte{[]byte("sports.news"), []byte("goal!")})
	intReply, ok := reply.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", reply)
	}
	if intReply.Code != 0 {
		t.Fatalf("expected 0 receivers, got %d", intReply.Code)
	}
}

func TestPubSubChannels(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	Subscribe(hub, c, [][]byte{[]byte("news.sports"), []byte("news.tech"), []byte("other")})

	reply := PubSub(hub, [][]byte{[]byte("channels")})
	mbr, ok := reply.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %T", reply)
	}
	if len(mbr.Args) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(mbr.Args))
	}

	reply = PubSub(hub, [][]byte{[]byte("channels"), []byte("news.*")})
	mbr, ok = reply.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %T", reply)
	}
	if len(mbr.Args) != 2 {
		t.Fatalf("expected 2 channels matching news.*, got %d", len(mbr.Args))
	}
}

func TestPubSubNumSub(t *testing.T) {
	hub := MakeHub()
	c1 := newFakeConn()
	c2 := newFakeConn()

	Subscribe(hub, c1, [][]byte{[]byte("chan1"), []byte("chan2")})
	Subscribe(hub, c2, [][]byte{[]byte("chan1")})

	reply := PubSub(hub, [][]byte{[]byte("numsub"), []byte("chan1"), []byte("chan2")})
	mbr, ok := reply.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %T", reply)
	}
	if len(mbr.Args) != 4 {
		t.Fatalf("expected 4 elements, got %d", len(mbr.Args))
	}
	if string(mbr.Args[0]) != "chan1" {
		t.Fatalf("expected 'chan1', got %q", string(mbr.Args[0]))
	}
	if string(mbr.Args[1]) != "2" {
		t.Fatalf("expected '2' subscribers for chan1, got %q", string(mbr.Args[1]))
	}
	if string(mbr.Args[2]) != "chan2" {
		t.Fatalf("expected 'chan2', got %q", string(mbr.Args[2]))
	}
	if string(mbr.Args[3]) != "1" {
		t.Fatalf("expected '1' subscriber for chan2, got %q", string(mbr.Args[3]))
	}
}

func TestPubSubNumPat(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	PSubscribe(hub, c, [][]byte{[]byte("a*"), []byte("b*")})

	reply := PubSub(hub, [][]byte{[]byte("numpat")})
	intReply, ok := reply.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", reply)
	}
	if intReply.Code != 2 {
		t.Fatalf("expected 2 unique patterns, got %d", intReply.Code)
	}
}

func TestPubSubUnknownSubcommand(t *testing.T) {
	hub := MakeHub()
	reply := PubSub(hub, [][]byte{[]byte("bogus")})
	if !strings.Contains(string(reply.ToBytes()), "unknown subcommand") {
		t.Fatalf("expected unknown subcommand error, got %q", string(reply.ToBytes()))
	}
}

func TestPubSubNoArgs(t *testing.T) {
	hub := MakeHub()
	reply := PubSub(hub, nil)
	if !strings.Contains(string(reply.ToBytes()), "wrong number of arguments") {
		t.Fatalf("expected wrong number of arguments error, got %q", string(reply.ToBytes()))
	}
}

func TestPSubscribeDuplicate(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	PSubscribe(hub, c, [][]byte{[]byte("a*")})
	PSubscribe(hub, c, [][]byte{[]byte("a*")})

	if c.PatternCount() != 1 {
		t.Fatalf("expected 1 pattern (dedup), got %d", c.PatternCount())
	}

	raw, ok := hub.patterns.Get("a*")
	if !ok {
		t.Fatal("expected pattern 'a*' in hub")
	}
	ll := raw.(*list.LinkedList)
	if ll.Len() != 1 {
		t.Fatalf("expected 1 subscriber in hub, got %d", ll.Len())
	}
}

func TestPublishToPatternQuestionMark(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	PSubscribe(hub, c, [][]byte{[]byte("h?llo")})

	reply := Publish(hub, [][]byte{[]byte("hello"), []byte("world")})
	intReply, ok := reply.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", reply)
	}
	if intReply.Code != 1 {
		t.Fatalf("expected 1 receiver for h?llo matching hello, got %d", intReply.Code)
	}

	// non-matching
	c.buf = nil
	reply = Publish(hub, [][]byte{[]byte("hllo"), []byte("world")})
	intReply, ok = reply.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", reply)
	}
	if intReply.Code != 0 {
		t.Fatalf("expected 0 receiver for h?llo not matching hllo, got %d", intReply.Code)
	}
}
