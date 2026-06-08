package stream

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	maxMs  = ^uint64(0)
	maxSeq = ^uint64(0)
)

// StreamID represents a stream entry ID (milliseconds-sequence)
type StreamID struct {
	Ms  uint64
	Seq uint64
}

// Greater returns true if a > b (lexicographic on Ms then Seq)
func (a StreamID) Greater(b StreamID) bool {
	return a.Ms > b.Ms || (a.Ms == b.Ms && a.Seq > b.Seq)
}

// String returns "ms-seq" format
func (id StreamID) String() string {
	return fmt.Sprintf("%d-%d", id.Ms, id.Seq)
}

// ParseID parses "ms-seq" or "ms" format
func ParseID(s string) (StreamID, error) {
	if s == "+" {
		return StreamID{maxMs, maxSeq}, nil
	}
	if s == "-" {
		return StreamID{0, 0}, nil
	}
	parts := strings.SplitN(s, "-", 2)
	ms, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return StreamID{}, errors.New("ERR Invalid stream ID specified as stream command argument")
	}
	if len(parts) == 1 {
		return StreamID{Ms: ms, Seq: 0}, nil
	}
	seq, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return StreamID{}, errors.New("ERR Invalid stream ID specified as stream command argument")
	}
	return StreamID{Ms: ms, Seq: seq}, nil
}

// Entry represents a single stream entry
type Entry struct {
	ID     StreamID
	Fields [][2]string // key-value pairs
}

// Stream is a behavior-faithful stream implementation.
// Layout is simplified (sorted slice). OBJECT ENCODING is always "stream".
type Stream struct {
	entries      []Entry
	lastID       StreamID
	maxDeletedID StreamID
	entriesAdded uint64
	length       uint64
	groups       map[string]*Group
}

// New creates a new empty stream
func New() *Stream {
	return &Stream{
		groups: map[string]*Group{},
	}
}

// Add appends an entry. If id has zero Ms/Seq, auto-assigns.
// Enforces monotonic IDs. Returns the assigned ID.
func (s *Stream) Add(id StreamID, fields [][2]string) (StreamID, error) {
	if id.Ms == 0 && id.Seq == 0 {
		// Auto-assign: use current time ms, seq = 0 or last+1
		id.Ms = s.lastID.Ms
		if id.Ms <= s.lastID.Ms {
			id.Ms = s.lastID.Ms
			id.Seq = s.lastID.Seq + 1
		}
	}

	// Enforce monotonic: new ID must be > lastID
	if !id.Greater(s.lastID) {
		return StreamID{}, errors.New("ERR The ID specified in XADD is equal or smaller than the target stream top item")
	}

	s.entries = append(s.entries, Entry{ID: id, Fields: fields})
	s.lastID = id
	s.entriesAdded++
	s.length++

	// Notify groups
	for _, g := range s.groups {
		g.NotifyNewEntry(id)
	}

	return id, nil
}

// Len returns the number of entries
func (s *Stream) Len() int {
	return int(s.length)
}

// LastID returns the last entry ID
func (s *Stream) LastID() StreamID {
	return s.lastID
}

// MaxDeletedID returns the max deleted ID
func (s *Stream) MaxDeletedID() StreamID {
	return s.maxDeletedID
}

// EntriesAdded returns the total number of entries ever added
func (s *Stream) EntriesAdded() uint64 {
	return s.entriesAdded
}

// Range returns entries in [start, end] range, up to count entries.
// count <= 0 means unlimited.
func (s *Stream) Range(start, end StreamID, count int) []Entry {
	var result []Entry
	for _, e := range s.entries {
		if (start.Ms == 0 && start.Seq == 0 || !start.Greater(e.ID)) &&
			(end.Ms == maxMs && end.Seq == maxSeq || !e.ID.Greater(end)) {
			result = append(result, e)
			if count > 0 && len(result) >= count {
				break
			}
		}
	}
	return result
}

// RevRange returns entries in [end, start] reverse range, up to count entries.
func (s *Stream) RevRange(start, end StreamID, count int) []Entry {
	var result []Entry
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if (start.Ms == 0 && start.Seq == 0 || !start.Greater(e.ID)) &&
			(end.Ms == maxMs && end.Seq == maxSeq || !e.ID.Greater(end)) {
			result = append(result, e)
			if count > 0 && len(result) >= count {
				break
			}
		}
	}
	return result
}

// Delete removes entries by ID. Returns count of actually deleted.
func (s *Stream) Delete(ids []StreamID) int {
	deleted := 0
	idSet := make(map[StreamID]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	filtered := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if idSet[e.ID] {
			deleted++
			if e.ID.Greater(s.maxDeletedID) {
				s.maxDeletedID = e.ID
			}
		} else {
			filtered = append(filtered, e)
		}
	}
	s.entries = filtered
	s.length -= uint64(deleted)
	return deleted
}

// TrimMaxLen keeps at most maxLen entries. Returns number removed.
// If approx is true, uses "~" semantics (can keep slightly more).
func (s *Stream) TrimMaxLen(maxLen int64, approx bool) int64 {
	if maxLen <= 0 || int64(len(s.entries)) <= maxLen {
		return 0
	}
	toRemove := int64(len(s.entries)) - maxLen
	if approx {
		// Remove in chunks for efficiency
		if toRemove > 100 {
			toRemove -= toRemove % 100
		} else {
			return 0
		}
	}
	removed := s.entries[:toRemove]
	s.entries = s.entries[toRemove:]
	s.length -= uint64(toRemove)
	// Update maxDeletedID
	if len(removed) > 0 {
		lastRemoved := removed[len(removed)-1].ID
		if lastRemoved.Greater(s.maxDeletedID) {
			s.maxDeletedID = lastRemoved
		}
	}
	return toRemove
}

// TrimMinID removes entries with ID < minID. Returns number removed.
func (s *Stream) TrimMinID(minID StreamID, approx bool) int64 {
	var removed int64
	var keep []Entry
	for _, e := range s.entries {
		if e.ID.Greater(minID) || e.ID == minID {
			keep = append(keep, e)
		} else {
			removed++
			if e.ID.Greater(s.maxDeletedID) {
				s.maxDeletedID = e.ID
			}
		}
	}
	if approx && removed < 100 {
		return 0
	}
	s.entries = keep
	s.length -= uint64(removed)
	return removed
}

// SetID sets the stream's last ID and entries added (for XSETID)
func (s *Stream) SetID(id StreamID, entriesAdded uint64, maxDeletedID StreamID) {
	s.lastID = id
	if entriesAdded > 0 {
		s.entriesAdded = entriesAdded
	}
	if maxDeletedID.Greater(s.maxDeletedID) {
		s.maxDeletedID = maxDeletedID
	}
}

// Groups returns the consumer groups map
func (s *Stream) Groups() map[string]*Group {
	return s.groups
}

// CreateGroup creates a new consumer group
func (s *Stream) CreateGroup(name string, startID StreamID) (*Group, error) {
	if _, exists := s.groups[name]; exists {
		return nil, errors.New("BUSYGROUP Consumer Group name already exists")
	}
	g := newGroup(name, startID)
	s.groups[name] = g
	return g, nil
}

// GetGroup returns a consumer group by name
func (s *Stream) GetGroup(name string) *Group {
	return s.groups[name]
}

// DestroyGroup removes a consumer group
func (s *Stream) DestroyGroup(name string) bool {
	if _, ok := s.groups[name]; !ok {
		return false
	}
	delete(s.groups, name)
	return true
}
