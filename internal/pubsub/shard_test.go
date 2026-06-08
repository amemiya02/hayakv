package pubsub

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func TestShardPublishDelivers(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	SSubscribe(hub, c, [][]byte{[]byte("sch")})
	n := SPublish(hub, [][]byte{[]byte("sch"), []byte("hi")})

	intReply, ok := n.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", n)
	}
	if intReply.Code != 1 {
		t.Fatalf("SPUBLISH count = %d, want 1", intReply.Code)
	}

	raw := string(c.buf)
	if !strings.Contains(raw, "smessage") {
		t.Fatalf("shard subscriber did not get smessage, got: %q", raw)
	}
	if !strings.Contains(raw, "sch") {
		t.Fatalf("shard subscriber did not get channel name, got: %q", raw)
	}
	if !strings.Contains(raw, "hi") {
		t.Fatalf("shard subscriber did not get message, got: %q", raw)
	}
}

func TestShardSubscribeConfirmation(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	SSubscribe(hub, c, [][]byte{[]byte("ch1"), []byte("ch2")})

	raw := string(c.buf)
	if strings.Count(raw, "ssubscribe") != 2 {
		t.Fatalf("expected 2 ssubscribe confirmations, got: %q", raw)
	}
}

func TestShardUnsubscribe(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	SSubscribe(hub, c, [][]byte{[]byte("sch")})
	c.buf = nil // clear buffer

	SUnsubscribe(hub, c, [][]byte{[]byte("sch")})

	raw := string(c.buf)
	if !strings.Contains(raw, "sunsubscribe") {
		t.Fatalf("expected sunsubscribe confirmation, got: %q", raw)
	}

	// After unsub, publishing should deliver to 0 clients
	n := SPublish(hub, [][]byte{[]byte("sch"), []byte("hi")})
	intReply, ok := n.(*protocol.IntReply)
	if !ok || intReply.Code != 0 {
		t.Fatalf("after unsub, SPUBLISH should return 0, got %d", intReply.Code)
	}
}

func TestShardUnsubscribeNothing(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	reply := SUnsubscribe(hub, c, nil)
	if _, ok := reply.(*protocol.NoReply); !ok {
		t.Fatalf("expected NoReply, got %T", reply)
	}
	if !strings.Contains(string(c.buf), "sunsubscribe") {
		t.Fatalf("expected sunsubscribe message in buffer, got %q", string(c.buf))
	}
}

func TestShardUnsubscribeAll(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	SSubscribe(hub, c, [][]byte{[]byte("sa"), []byte("sb"), []byte("sc")})
	SUnsubscribeAll(hub, c)
	if c.SubsCount() != 0 {
		t.Fatalf("expected 0 subs after unsub all, got %d", c.SubsCount())
	}
	if hub.shardSubs.Len() != 0 {
		t.Fatalf("expected hub.shardSubs len 0 after unsub all, got %d", hub.shardSubs.Len())
	}
}

func TestShardSubscribeDuplicate(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	SSubscribe(hub, c, [][]byte{[]byte("sa")})
	SSubscribe(hub, c, [][]byte{[]byte("sa")})

	if c.SubsCount() != 1 {
		t.Fatalf("expected 1 sub (dedup), got %d", c.SubsCount())
	}

	raw, ok := hub.shardSubs.Get("sa")
	if !ok {
		t.Fatal("expected shard channel 'sa' in hub")
	}
	ll := raw.(*list.LinkedList)
	if ll.Len() != 1 {
		t.Fatalf("expected 1 subscriber in hub, got %d", ll.Len())
	}
}

func TestShardPubSubChannels(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	SSubscribe(hub, c, [][]byte{[]byte("snews"), []byte("sevents")})

	reply := PubSub(hub, [][]byte{[]byte("shardchannels")})
	mbr, ok := reply.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %T", reply)
	}
	if len(mbr.Args) != 2 {
		t.Fatalf("expected 2 shard channels, got %d", len(mbr.Args))
	}
}

func TestShardPubSubChannelsWithPattern(t *testing.T) {
	hub := MakeHub()
	c := newFakeConn()

	SSubscribe(hub, c, [][]byte{[]byte("snews.sports"), []byte("snews.tech"), []byte("sother")})

	reply := PubSub(hub, [][]byte{[]byte("shardchannels"), []byte("snews.*")})
	mbr, ok := reply.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %T", reply)
	}
	if len(mbr.Args) != 2 {
		t.Fatalf("expected 2 shard channels matching snews.*, got %d", len(mbr.Args))
	}
}

func TestShardPubSubNumSub(t *testing.T) {
	hub := MakeHub()
	c1 := newFakeConn()
	c2 := newFakeConn()

	SSubscribe(hub, c1, [][]byte{[]byte("s1"), []byte("s2")})
	SSubscribe(hub, c2, [][]byte{[]byte("s1")})

	reply := PubSub(hub, [][]byte{[]byte("shardnumsub"), []byte("s1"), []byte("s2")})
	mbr, ok := reply.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %T", reply)
	}
	if len(mbr.Args) != 4 {
		t.Fatalf("expected 4 elements, got %d", len(mbr.Args))
	}
	if string(mbr.Args[0]) != "s1" {
		t.Fatalf("expected 's1', got %q", string(mbr.Args[0]))
	}
	if string(mbr.Args[1]) != "2" {
		t.Fatalf("expected '2' subscribers for s1, got %q", string(mbr.Args[1]))
	}
	if string(mbr.Args[2]) != "s2" {
		t.Fatalf("expected 's2', got %q", string(mbr.Args[2]))
	}
	if string(mbr.Args[3]) != "1" {
		t.Fatalf("expected '1' subscriber for s2, got %q", string(mbr.Args[3]))
	}
}

func TestShardPubSubNumSubEmpty(t *testing.T) {
	hub := MakeHub()

	reply := PubSub(hub, [][]byte{[]byte("shardnumsub"), []byte("snoexist")})
	mbr, ok := reply.(*protocol.MultiBulkReply)
	if !ok {
		t.Fatalf("expected MultiBulkReply, got %T", reply)
	}
	if len(mbr.Args) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(mbr.Args))
	}
	if string(mbr.Args[0]) != "snoexist" {
		t.Fatalf("expected 'snoexist', got %q", string(mbr.Args[0]))
	}
	if string(mbr.Args[1]) != "0" {
		t.Fatalf("expected '0' subscribers, got %q", string(mbr.Args[1]))
	}
}

func TestShardPublishNoSubscribers(t *testing.T) {
	hub := MakeHub()

	n := SPublish(hub, [][]byte{[]byte("snoexist"), []byte("hi")})
	intReply, ok := n.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", n)
	}
	if intReply.Code != 0 {
		t.Fatalf("SPUBLISH to no subscribers should return 0, got %d", intReply.Code)
	}
}

func TestShardPublishArgError(t *testing.T) {
	hub := MakeHub()

	reply := SPublish(hub, [][]byte{[]byte("onlyone")})
	if !strings.Contains(string(reply.ToBytes()), "wrong number of arguments") {
		t.Fatalf("expected arg count error, got %q", string(reply.ToBytes()))
	}
}

func TestShardMultipleSubscribers(t *testing.T) {
	hub := MakeHub()
	c1 := newFakeConn()
	c2 := newFakeConn()
	c3 := newFakeConn()

	SSubscribe(hub, c1, [][]byte{[]byte("schan")})
	SSubscribe(hub, c2, [][]byte{[]byte("schan")})
	SSubscribe(hub, c3, [][]byte{[]byte("schan")})

	n := SPublish(hub, [][]byte{[]byte("schan"), []byte("hello")})
	intReply, ok := n.(*protocol.IntReply)
	if !ok {
		t.Fatalf("expected IntReply, got %T", n)
	}
	if intReply.Code != 3 {
		t.Fatalf("SPUBLISH count = %d, want 3", intReply.Code)
	}
}
