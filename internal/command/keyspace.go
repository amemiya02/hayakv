package database

import (
	"fmt"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/pubsub"
)

// Keyspace notification flag bits. The low bits are the class flags (g$lshzxetmn),
// matching Redis notify-keyspace-events semantics. The two high discriminator bits
// (K, E) control whether __keyspace@db__:key and __keyevent@db__:event channels
// are published to, respectively.
const (
	notifyKeyspaceBit = 1 << iota // K — publish to __keyspace@db__:key
	notifyKeyeventBit             // E — publish to __keyevent@db__:event
	notifyGeneric                 // g — generic commands (DEL, EXPIRE, RENAME, …)
	notifyString                  // $ — string commands
	notifyList                    // l — list commands
	notifySet                     // s — set commands
	notifyHash                    // h — hash commands
	notifyZset                    // z — sorted-set commands
	notifyExpired                 // x — expired events
	notifyEvicted                 // e — evicted events
	notifyStream                  // t — stream commands
	notifyKeyMiss                 // m — key miss events
	notifyNew                     // n — new key events
)

// notifyFlags is the parsed bit-field representation of the
// notify-keyspace-events config string.
type notifyFlags int

func (f notifyFlags) enabled() bool    { return f != 0 }
func (f notifyFlags) keyspace() bool   { return f&notifyKeyspaceBit != 0 }
func (f notifyFlags) keyevent() bool   { return f&notifyKeyeventBit != 0 }
func (f notifyFlags) has(bit int) bool { return f&notifyFlags(bit) != 0 }

// parseNotifyFlags maps the redis.conf string (e.g. "KEA", "Elg") to a
// notifyFlags bit-field.
//
//   - 'K' sets the keyspace discriminator bit
//   - 'E' sets the keyevent discriminator bit
//   - 'A' expands to all class bits: g $ l s h z x e t (everything except m and n)
//   - 'g' = generic, '$' = string, 'l' = list, 's' = set, 'h' = hash, 'z' = zset
//   - 'x' = expired, 'e' = evicted, 't' = stream
//   - 'm' = keymiss, 'n' = new
func parseNotifyFlags(s string) notifyFlags {
	var f notifyFlags
	for _, ch := range s {
		switch ch {
		case 'K':
			f |= notifyKeyspaceBit
		case 'E':
			f |= notifyKeyeventBit
		case 'A':
			f |= notifyGeneric | notifyString | notifyList | notifySet |
				notifyHash | notifyZset | notifyExpired | notifyEvicted | notifyStream
		case 'g':
			f |= notifyGeneric
		case '$':
			f |= notifyString
		case 'l':
			f |= notifyList
		case 's':
			f |= notifySet
		case 'h':
			f |= notifyHash
		case 'z':
			f |= notifyZset
		case 'x':
			f |= notifyExpired
		case 'e':
			f |= notifyEvicted
		case 't':
			f |= notifyStream
		case 'm':
			f |= notifyKeyMiss
		case 'n':
			f |= notifyNew
		}
	}
	return f
}

// validNotifyFlags checks whether the given string is a valid notify-keyspace-events
// configuration value. Empty string is valid (disabled). Each character must be one
// of the recognized flag letters: K, E, A, g, $, l, s, h, z, x, e, t, m, n.
func validNotifyFlags(s string) bool {
	for _, ch := range s {
		switch ch {
		case 'K', 'E', 'A', 'g', '$', 'l', 's', 'h', 'z', 'x', 'e', 't', 'm', 'n':
			// ok
		default:
			return false
		}
	}
	return true
}

// notifyKeyspaceEvent emits a keyspace notification if the event class is
// enabled in the current configuration. It publishes to both
// __keyspace@db__:key (payload = event name) and __keyevent@db__:event
// (payload = key name) channels, gated by the K/E discriminator bits.
func (server *Server) notifyKeyspaceEvent(dbIndex int, classBit int, event, key string) {
	f := parseNotifyFlags(config.Properties.NotifyKeyspaceEvents)
	if !f.enabled() || !f.has(classBit) {
		return
	}
	if f.keyspace() {
		ch := fmt.Sprintf("__keyspace@%d__:%s", dbIndex, key)
		pubsub.Publish(server.hub, [][]byte{[]byte(ch), []byte(event)})
	}
	if f.keyevent() {
		ch := fmt.Sprintf("__keyevent@%d__:%s", dbIndex, event)
		pubsub.Publish(server.hub, [][]byte{[]byte(ch), []byte(key)})
	}
}
