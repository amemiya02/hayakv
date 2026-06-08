package database

import (
	"strings"
	"testing"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/pubsub"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestNotifyFlagsParse(t *testing.T) {
	// "KEA" should enable keyspace + keyevent + all class bits
	f := parseNotifyFlags("KEA")
	if !f.enabled() {
		t.Fatal("KEA should be enabled")
	}
	if !f.keyspace() {
		t.Fatal("KEA should have keyspace bit")
	}
	if !f.keyevent() {
		t.Fatal("KEA should have keyevent bit")
	}
	// All class bits should be set by 'A'
	for _, bit := range []int{notifyGeneric, notifyString, notifyList, notifySet, notifyHash, notifyZset, notifyExpired, notifyEvicted, notifyStream} {
		if !f.has(bit) {
			t.Fatalf("KEA should have class bit %d", bit)
		}
	}
	// 'A' does NOT set notifyKeyMiss or notifyNew
	if f.has(notifyKeyMiss) {
		t.Fatal("KEA should not have keymiss bit")
	}
	if f.has(notifyNew) {
		t.Fatal("KEA should not have new bit")
	}

	// "Elg" = keyevent + list + generic
	g := parseNotifyFlags("Elg")
	if g.keyspace() {
		t.Fatal("Elg should not have keyspace bit")
	}
	if !g.keyevent() {
		t.Fatal("Elg should have keyevent bit")
	}
	if !g.has(notifyGeneric) {
		t.Fatal("Elg should have generic bit")
	}
	if !g.has(notifyList) {
		t.Fatal("Elg should have list bit")
	}
	if g.has(notifyString) {
		t.Fatal("Elg should not have string bit")
	}

	// Empty string = disabled
	z := parseNotifyFlags("")
	if z.enabled() {
		t.Fatal("empty should be disabled")
	}

	// "K$" = keyspace + string only
	kd := parseNotifyFlags("K$")
	if !kd.keyspace() {
		t.Fatal("K$ should have keyspace bit")
	}
	if kd.keyevent() {
		t.Fatal("K$ should not have keyevent bit")
	}
	if !kd.has(notifyString) {
		t.Fatal("K$ should have string bit")
	}
	if kd.has(notifyGeneric) {
		t.Fatal("K$ should not have generic bit")
	}

	// "Ex" = keyevent + expired
	ex := parseNotifyFlags("Ex")
	if ex.keyspace() {
		t.Fatal("Ex should not have keyspace bit")
	}
	if !ex.keyevent() {
		t.Fatal("Ex should have keyevent bit")
	}
	if !ex.has(notifyExpired) {
		t.Fatal("Ex should have expired bit")
	}
}

func TestNotifyFlagsIndividualBits(t *testing.T) {
	cases := []struct {
		input string
		bit   int
	}{
		{"g", notifyGeneric},
		{"$", notifyString},
		{"l", notifyList},
		{"s", notifySet},
		{"h", notifyHash},
		{"z", notifyZset},
		{"x", notifyExpired},
		{"e", notifyEvicted},
		{"t", notifyStream},
		{"m", notifyKeyMiss},
		{"n", notifyNew},
	}
	for _, tc := range cases {
		f := parseNotifyFlags(tc.input)
		if !f.has(tc.bit) {
			t.Fatalf("flag '%s' should set bit %d", tc.input, tc.bit)
		}
		// Should NOT have other class bits
		for _, other := range cases {
			if other.bit != tc.bit && f.has(other.bit) {
				t.Fatalf("flag '%s' should not set bit %d", tc.input, other.bit)
			}
		}
	}
}

func TestEmitPublishesBothChannels(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	config.Properties.NotifyKeyspaceEvents = "KEA"
	defer func() { config.Properties.NotifyKeyspaceEvents = "" }()

	conn := connection.NewFakeConn()
	// Subscribe to both pattern channels
	pubsub.PSubscribe(s.hub, conn, [][]byte{
		[]byte("__keyevent@0__:*"),
		[]byte("__keyspace@0__:*"),
	})
	// Clear subscription confirmation bytes so we only see notification messages
	conn.Clean()

	// Emit a "set" event on key "mykey" in db 0
	s.notifyKeyspaceEvent(0, notifyString, "set", "mykey")

	// Check that data was written to the fake connection
	written := string(conn.Bytes())
	if written == "" {
		t.Fatal("expected pub/sub messages to be written to connection")
	}

	// Should contain pmessage with keyspace channel pattern
	if !strings.Contains(written, "__keyspace@0__:mykey") {
		t.Fatalf("expected keyspace channel in output, got: %s", written)
	}
	// Should contain pmessage with keyevent channel pattern
	if !strings.Contains(written, "__keyevent@0__:set") {
		t.Fatalf("expected keyevent channel in output, got: %s", written)
	}
	// The keyspace channel message body should be the event name "set"
	// The keyevent channel message body should be the key name "mykey"
	if !strings.Contains(written, "set") {
		t.Fatalf("expected event name 'set' in output, got: %s", written)
	}
	if !strings.Contains(written, "mykey") {
		t.Fatalf("expected key name 'mykey' in output, got: %s", written)
	}
}

func TestEmitDisabledWhenNoFlags(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	config.Properties.NotifyKeyspaceEvents = ""
	defer func() { config.Properties.NotifyKeyspaceEvents = "" }()

	conn := connection.NewFakeConn()
	pubsub.PSubscribe(s.hub, conn, [][]byte{
		[]byte("__keyevent@0__:*"),
		[]byte("__keyspace@0__:*"),
	})
	conn.Clean()

	s.notifyKeyspaceEvent(0, notifyString, "set", "mykey")

	written := conn.Bytes()
	if len(written) > 0 {
		t.Fatalf("expected no output when notifications disabled, got: %s", string(written))
	}
}

func TestEmitSkipsWrongClass(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	// Only enable keyevent + list (not string)
	config.Properties.NotifyKeyspaceEvents = "El"
	defer func() { config.Properties.NotifyKeyspaceEvents = "" }()

	conn := connection.NewFakeConn()
	pubsub.PSubscribe(s.hub, conn, [][]byte{
		[]byte("__keyevent@0__:*"),
	})
	conn.Clean()

	// Emit a string-class event — should be skipped
	s.notifyKeyspaceEvent(0, notifyString, "set", "mykey")
	if len(conn.Bytes()) > 0 {
		t.Fatalf("expected no output for string event with only list enabled, got: %s", string(conn.Bytes()))
	}

	// Emit a list-class event — should be delivered
	s.notifyKeyspaceEvent(0, notifyList, "lpush", "mylist")
	if len(conn.Bytes()) == 0 {
		t.Fatal("expected output for list event with list enabled")
	}
}

func TestEmitKeyspaceOnly(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	// Only keyspace channel, not keyevent
	config.Properties.NotifyKeyspaceEvents = "K$"
	defer func() { config.Properties.NotifyKeyspaceEvents = "" }()

	conn := connection.NewFakeConn()
	pubsub.PSubscribe(s.hub, conn, [][]byte{
		[]byte("__keyspace@0__:*"),
		[]byte("__keyevent@0__:*"),
	})
	conn.Clean()

	s.notifyKeyspaceEvent(0, notifyString, "set", "mykey")

	written := string(conn.Bytes())
	if !strings.Contains(written, "__keyspace@0__:mykey") {
		t.Fatalf("expected keyspace channel, got: %s", written)
	}
	if strings.Contains(written, "__keyevent@0__:set") {
		t.Fatalf("should not contain keyevent channel with only K flag, got: %s", written)
	}
}

func TestValidNotifyFlags(t *testing.T) {
	// Valid values
	valid := []string{"", "K", "E", "KEA", "Elg", "K$", "Ex", "A", "g$lshzxetmn"}
	for _, v := range valid {
		if !validNotifyFlags(v) {
			t.Fatalf("expected '%s' to be valid", v)
		}
	}
	// Invalid values
	invalid := []string{"KX", "abc", "KE1", "K A"}
	for _, v := range invalid {
		if validNotifyFlags(v) {
			t.Fatalf("expected '%s' to be invalid", v)
		}
	}
}

func TestConfigSetNotifyKeyspaceEvents(t *testing.T) {
	s := NewStandaloneServer()
	defer s.Close()
	c := connection.NewFakeConn()
	defer func() { config.Properties.NotifyKeyspaceEvents = "" }()

	// CONFIG SET with valid value
	reply := s.Exec(c, [][]byte{[]byte("CONFIG"), []byte("SET"), []byte("notify-keyspace-events"), []byte("KEA")})
	if protocol.IsErrorReply(reply) {
		t.Fatalf("CONFIG SET notify-keyspace-events KEA should succeed, got: %s", string(reply.ToBytes()))
	}
	if config.Properties.NotifyKeyspaceEvents != "KEA" {
		t.Fatalf("expected config value 'KEA', got '%s'", config.Properties.NotifyKeyspaceEvents)
	}

	// CONFIG SET with invalid value
	reply = s.Exec(c, [][]byte{[]byte("CONFIG"), []byte("SET"), []byte("notify-keyspace-events"), []byte("XYZ")})
	if !protocol.IsErrorReply(reply) {
		t.Fatal("CONFIG SET notify-keyspace-events XYZ should fail")
	}

	// CONFIG GET should return the value
	reply = s.Exec(c, [][]byte{[]byte("CONFIG"), []byte("GET"), []byte("notify-keyspace-events")})
	if protocol.IsErrorReply(reply) {
		t.Fatalf("CONFIG GET notify-keyspace-events should succeed, got: %s", string(reply.ToBytes()))
	}
	// The reply should be a multi-bulk with [name, value]
	bytes := reply.ToBytes()
	if !strings.Contains(string(bytes), "notify-keyspace-events") {
		t.Fatalf("expected 'notify-keyspace-events' in CONFIG GET reply, got: %s", string(bytes))
	}
	if !strings.Contains(string(bytes), "KEA") {
		t.Fatalf("expected 'KEA' in CONFIG GET reply, got: %s", string(bytes))
	}
}
