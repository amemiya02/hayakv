package stream

import (
	"time"
)

// PendingEntry represents an entry in the PEL (Pending Entry List)
type PendingEntry struct {
	Consumer      string
	DeliveryTime  int64
	DeliveryCount uint64
}

// Consumer represents a stream consumer
type Consumer struct {
	Name       string
	Pending    map[StreamID]*PendingEntry
	ActiveTime int64
}

// Group represents a consumer group
type Group struct {
	Name          string
	lastDelivered StreamID
	entriesRead   uint64
	pending       map[StreamID]*PendingEntry
	consumers     map[string]*Consumer
}

func newGroup(name string, startID StreamID) *Group {
	return &Group{
		Name:          name,
		lastDelivered: startID,
		pending:       map[StreamID]*PendingEntry{},
		consumers:     map[string]*Consumer{},
	}
}

// LastDelivered returns the last delivered ID
func (g *Group) LastDelivered() StreamID {
	return g.lastDelivered
}

// NotifyNewEntry updates group state when a new entry is added to the stream
func (g *Group) NotifyNewEntry(id StreamID) {
	// Just track; delivery happens on XREADGROUP
}

// ReadNew delivers new entries (those with ID > lastDelivered) to the consumer.
// Returns the entries delivered.
func (g *Group) ReadNew(s *Stream, consumerName string, count int) []Entry {
	start := StreamID{g.lastDelivered.Ms, g.lastDelivered.Seq + 1}
	if g.lastDelivered.Ms == 0 && g.lastDelivered.Seq == 0 {
		start = StreamID{0, 0} // from beginning
	}

	entries := s.Range(start, StreamID{maxMs, maxSeq}, count)
	if len(entries) == 0 {
		return nil
	}

	consumer := g.getOrCreateConsumer(consumerName)
	now := time.Now().UnixMilli()

	for _, e := range entries {
		pe := &PendingEntry{
			Consumer:      consumerName,
			DeliveryTime:  now,
			DeliveryCount: 1,
		}
		g.pending[e.ID] = pe
		consumer.Pending[e.ID] = pe
		consumer.ActiveTime = now
		if e.ID.Greater(g.lastDelivered) {
			g.lastDelivered = e.ID
		}
	}
	g.entriesRead += uint64(len(entries))

	return entries
}

// ReadPending delivers pending entries for the given consumer.
func (g *Group) ReadPending(consumerName string, start, end StreamID, count int) []Entry {
	consumer, ok := g.consumers[consumerName]
	if !ok {
		return nil
	}

	var result []Entry
	for id, pe := range consumer.Pending {
		if pe.Consumer != consumerName {
			continue
		}
		if !id.Greater(start) && !(id.Ms == start.Ms && id.Seq == start.Seq) {
			continue
		}
		if id.Greater(end) {
			continue
		}
		result = append(result, Entry{ID: id})
		if count > 0 && len(result) >= count {
			break
		}
	}
	return result
}

// Ack acknowledges entries, removing them from the PEL.
func (g *Group) Ack(ids []StreamID) int {
	acked := 0
	for _, id := range ids {
		pe, ok := g.pending[id]
		if !ok {
			continue
		}
		delete(g.pending, id)
		if c, ok := g.consumers[pe.Consumer]; ok {
			delete(c.Pending, id)
		}
		acked++
	}
	return acked
}

// PendingCount returns the number of pending entries
func (g *Group) PendingCount() int {
	return len(g.pending)
}

// getOrCreateConsumer gets or creates a consumer
func (g *Group) getOrCreateConsumer(name string) *Consumer {
	c, ok := g.consumers[name]
	if !ok {
		c = &Consumer{
			Name:    name,
			Pending: map[StreamID]*PendingEntry{},
		}
		g.consumers[name] = c
	}
	return c
}

// Consumers returns the consumers map
func (g *Group) Consumers() map[string]*Consumer {
	return g.consumers
}

// Claim transfers ownership of pending entries to a new consumer
func (g *Group) Claim(newConsumer string, minIdleTime int64, ids []StreamID, force bool) []StreamID {
	now := time.Now().UnixMilli()
	consumer := g.getOrCreateConsumer(newConsumer)
	var claimed []StreamID

	for _, id := range ids {
		pe, ok := g.pending[id]
		if !ok && !force {
			continue
		}
		if ok && !force && now-pe.DeliveryTime < minIdleTime {
			continue
		}

		// Remove from old consumer
		if ok {
			if oldC, ok := g.consumers[pe.Consumer]; ok {
				delete(oldC.Pending, id)
			}
		}

		// Assign to new consumer
		if !ok {
			pe = &PendingEntry{}
			g.pending[id] = pe
		}
		pe.Consumer = newConsumer
		pe.DeliveryTime = now
		pe.DeliveryCount++
		consumer.Pending[id] = pe
		consumer.ActiveTime = now
		claimed = append(claimed, id)
	}
	return claimed
}

// SetID resets the group's last delivered ID
func (g *Group) SetID(id StreamID) {
	g.lastDelivered = id
}

// Lag calculates the lag (entries added - entries read)
func (g *Group) Lag(streamEntriesAdded uint64) uint64 {
	if g.entriesRead > streamEntriesAdded {
		return 0
	}
	return streamEntriesAdded - g.entriesRead
}

// PendingMap returns the group's pending entry list (PEL) for RDB encoding.
func (g *Group) PendingMap() map[StreamID]*PendingEntry {
	return g.pending
}

// EntriesRead returns the number of entries read by this group.
func (g *Group) EntriesRead() uint64 {
	return g.entriesRead
}

// SetEntriesRead sets the entriesRead counter (used during RDB restore).
func (g *Group) SetEntriesRead(n uint64) {
	g.entriesRead = n
}

// RestorePending adds a pending entry to the group's PEL (used during RDB restore).
func (g *Group) RestorePending(id StreamID, consumerName string, deliveryTime int64, deliveryCount uint64) {
	pe := &PendingEntry{
		Consumer:      consumerName,
		DeliveryTime:  deliveryTime,
		DeliveryCount: deliveryCount,
	}
	g.pending[id] = pe
	c := g.getOrCreateConsumer(consumerName)
	c.Pending[id] = pe
}
