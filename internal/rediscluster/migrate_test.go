package rediscluster

import (
	"bytes"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/amemiya02/hayakv/internal/iface"
	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

type missEngine struct{} // always reports key-miss (nil bulk) so ASK triggers

func (missEngine) Exec(_ iredis.Connection, _ iface.CmdLine) iredis.Reply {
	return protocol.MakeNullBulkReply()
}
func (missEngine) AfterClientClose(iredis.Connection) {}
func (missEngine) Close()                             {}

func TestSetSlotMigratingThenAsk(t *testing.T) {
	st := newClusterState("127.0.0.1", 7000, filepath.Join(t.TempDir(), "nodes.conf"))
	st.addSlots([]uint16{Key2Slot("foo")}) // we own foo's slot...
	target := newNode(genNodeID(), "10.0.0.5", 7003)
	st.mu.Lock()
	st.nodes[target.id] = target
	st.mu.Unlock()
	ce := NewClusterEngine(missEngine{}, st)
	// Mark the slot MIGRATING to target.
	r := resp2.Codec{}.Encode(ce.Exec(nil, cmd("CLUSTER", "SETSLOT",
		itoa(Key2Slot("foo")), "MIGRATING", target.id)), iredis.RESP2)
	if string(r) != "+OK\r\n" {
		t.Fatalf("SETSLOT MIGRATING = %q", r)
	}
	// We still own the slot, but the key is missing locally -> ASK to target.
	got := resp2.Codec{}.Encode(ce.Exec(nil, cmd("GET", "foo")), iredis.RESP2)
	want := []byte("-ASK " + itoa(Key2Slot("foo")) + " 10.0.0.5:7003\r\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("ASK reply = %q, want %q", got, want)
	}
}

func TestImportingServesOnlyWithAsking(t *testing.T) {
	st := newClusterState("127.0.0.1", 7000, filepath.Join(t.TempDir(), "nodes.conf"))
	source := newNode(genNodeID(), "10.0.0.1", 7004)
	slot := Key2Slot("foo")
	st.mu.Lock()
	st.nodes[source.id] = source
	st.slots[slot] = source // source owns it; we are merely importing
	source.addSlot(slot)
	st.mu.Unlock()
	// We are IMPORTING the slot from source.
	ce := NewClusterEngine(okEngine{}, st)
	r := resp2.Codec{}.Encode(ce.Exec(nil, cmd("CLUSTER", "SETSLOT",
		itoa(slot), "IMPORTING", source.id)), iredis.RESP2)
	if string(r) != "+OK\r\n" {
		t.Fatalf("SETSLOT IMPORTING = %q", r)
	}
	// Without ASKING: we are not the owner -> MOVED to source.
	got := resp2.Codec{}.Encode(ce.Exec(connStub("c1"), cmd("GET", "foo")), iredis.RESP2)
	if !bytes.HasPrefix(got, []byte("-MOVED")) {
		t.Fatalf("no ASKING should MOVED, got %q", got)
	}
	// With ASKING (one-shot): served locally.
	c := connStub("c1")
	_ = ce.Exec(c, cmd("ASKING"))
	got = resp2.Codec{}.Encode(ce.Exec(c, cmd("GET", "foo")), iredis.RESP2)
	if string(got) != "+OK\r\n" {
		t.Fatalf("ASKING+GET should serve locally, got %q", got)
	}
}

func itoa(s uint16) string { return strconv.Itoa(int(s)) }

type okEngine struct{}

func (okEngine) Exec(_ iredis.Connection, _ iface.CmdLine) iredis.Reply {
	return protocol.MakeOkReply()
}
func (okEngine) AfterClientClose(iredis.Connection) {}
func (okEngine) Close()                             {}

// connStub is a minimal iredis.Connection with a fixed remote addr (for ASKING).
type connStub string

func (connStub) Write([]byte) (int, error)      { return 0, nil }
func (connStub) Close() error                   { return nil }
func (c connStub) RemoteAddr() string           { return string(c) }
func (connStub) SetPassword(string)             {}
func (connStub) GetPassword() string            { return "" }
func (connStub) Subscribe(string)               {}
func (connStub) UnSubscribe(string)             {}
func (connStub) SubsCount() int                 { return 0 }
func (connStub) GetChannels() []string          { return nil }
func (connStub) PSubscribe(string)              {}
func (connStub) PUnSubscribe(string)            {}
func (connStub) PatternCount() int              { return 0 }
func (connStub) GetPatterns() []string          { return nil }
func (connStub) InMultiState() bool             { return false }
func (connStub) SetMultiState(bool)             {}
func (connStub) GetQueuedCmdLine() [][][]byte   { return nil }
func (connStub) EnqueueCmd([][]byte)            {}
func (connStub) ClearQueuedCmds()               {}
func (connStub) GetWatching() map[string]uint32 { return nil }
func (connStub) AddTxError(error)               {}
func (connStub) GetTxErrors() []error           { return nil }
func (connStub) GetDBIndex() int                { return 0 }
func (connStub) SelectDB(int)                   {}
func (connStub) Protocol() iredis.RespVersion   { return iredis.RESP2 }
func (connStub) SetProtocol(iredis.RespVersion) {}
func (connStub) SetSlave()                      {}
func (connStub) IsSlave() bool                  { return false }
func (connStub) SetMaster()                     {}
func (connStub) IsMaster() bool                 { return false }
func (connStub) Name() string                   { return "" }
func (connStub) ClientID() uint64               { return 0 }
func (connStub) ClientName() string             { return "" }
func (connStub) SetClientName(string)           {}
func (connStub) LibName() string                { return "" }
func (connStub) SetLibName(string)              {}
func (connStub) LibVer() string                 { return "" }
func (connStub) SetLibVer(string)               {}
func (connStub) CreatedAt() time.Time           { return time.Time{} }
func (connStub) ReplyMode() int                 { return 0 }
func (connStub) SetReplyMode(int)               {}
func (connStub) IsTracking() bool               { return false }
func (connStub) SetTracking(bool)               {}
func (connStub) TrackingMode() int              { return 0 }
func (connStub) SetTrackingMode(int)            {}
func (connStub) NoLoop() bool                   { return false }
func (connStub) SetNoLoop(bool)                 {}
func (connStub) RedirectID() uint64             { return 0 }
func (connStub) SetRedirectID(uint64)           {}
func (connStub) BcastMode() bool                { return false }
func (connStub) SetBcastMode(bool)              {}
func (connStub) BcastPrefixes() []string        { return nil }
func (connStub) SetBcastPrefixes([]string)      {}
func (connStub) CachingNext() bool              { return false }
func (connStub) SetCachingNext(bool)            {}
