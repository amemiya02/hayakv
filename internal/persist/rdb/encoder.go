package rdb

import (
	"encoding/binary"
	"io"

	"github.com/amemiya02/hayakv/internal/datastruct/stream"
)

// Encoder writes an RDB v12 stream and finalizes a CRC64 trailer.
type Encoder struct {
	w *writer
}

// NewEncoder returns an Encoder writing to w.
func NewEncoder(w io.Writer) *Encoder { return &Encoder{w: newWriter(w)} }

// WriteHeader writes the "REDIS0012" magic (RDB version 12, matching Redis 8.x).
func (e *Encoder) WriteHeader() error {
	return e.w.writeBytes([]byte("REDIS0012"))
}

// WriteAux writes a 0xFA auxiliary key/value field.
func (e *Encoder) WriteAux(key, val string) error {
	if err := e.w.writeByte(opAux); err != nil {
		return err
	}
	if err := e.w.writeString([]byte(key)); err != nil {
		return err
	}
	return e.w.writeString([]byte(val))
}

// WriteSelectDB writes a 0xFE SELECTDB opcode.
func (e *Encoder) WriteSelectDB(db int) error {
	if err := e.w.writeByte(opSelectDB); err != nil {
		return err
	}
	return e.w.writeLen(uint64(db))
}

// WriteResizeDB writes a 0xFB RESIZEDB hint (db size, expires size).
func (e *Encoder) WriteResizeDB(dbSize, expiresSize uint64) error {
	if err := e.w.writeByte(opResizeDB); err != nil {
		return err
	}
	if err := e.w.writeLen(dbSize); err != nil {
		return err
	}
	return e.w.writeLen(expiresSize)
}

// writeExpire emits 0xFC + 8-byte LE ms when expireMS != 0.
func (e *Encoder) writeExpire(expireMS uint64) error {
	if expireMS == 0 {
		return nil
	}
	if err := e.w.writeByte(opExpireMS); err != nil {
		return err
	}
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], expireMS)
	return e.w.writeBytes(b[:])
}

// WriteStringEntry writes [expire?] typeString key value.
func (e *Encoder) WriteStringEntry(key, val []byte, expireMS uint64) error {
	if err := e.writeExpire(expireMS); err != nil {
		return err
	}
	if err := e.w.writeByte(typeString); err != nil {
		return err
	}
	if err := e.w.writeString(key); err != nil {
		return err
	}
	return e.w.writeString(val)
}

// WriteListEntry writes [expire?] typeList key len(vals) vals...
func (e *Encoder) WriteListEntry(key []byte, vals [][]byte, expireMS uint64) error {
	if err := e.writeExpire(expireMS); err != nil {
		return err
	}
	if err := e.w.writeByte(typeList); err != nil {
		return err
	}
	if err := e.w.writeString(key); err != nil {
		return err
	}
	if err := e.w.writeLen(uint64(len(vals))); err != nil {
		return err
	}
	for _, v := range vals {
		if err := e.w.writeString(v); err != nil {
			return err
		}
	}
	return nil
}

// WriteSetEntry writes [expire?] typeSet key len(members) members...
func (e *Encoder) WriteSetEntry(key []byte, members [][]byte, expireMS uint64) error {
	if err := e.writeExpire(expireMS); err != nil {
		return err
	}
	if err := e.w.writeByte(typeSet); err != nil {
		return err
	}
	if err := e.w.writeString(key); err != nil {
		return err
	}
	if err := e.w.writeLen(uint64(len(members))); err != nil {
		return err
	}
	for _, m := range members {
		if err := e.w.writeString(m); err != nil {
			return err
		}
	}
	return nil
}

// WriteHashEntry writes [expire?] typeHash key len(pairs) (field value)...
func (e *Encoder) WriteHashEntry(key []byte, hash map[string][]byte, expireMS uint64) error {
	if err := e.writeExpire(expireMS); err != nil {
		return err
	}
	if err := e.w.writeByte(typeHash); err != nil {
		return err
	}
	if err := e.w.writeString(key); err != nil {
		return err
	}
	if err := e.w.writeLen(uint64(len(hash))); err != nil {
		return err
	}
	for f, v := range hash {
		if err := e.w.writeString([]byte(f)); err != nil {
			return err
		}
		if err := e.w.writeString(v); err != nil {
			return err
		}
	}
	return nil
}

// WriteZSetEntry writes [expire?] typeZSet key len(members) (member score-as-string)...
func (e *Encoder) WriteZSetEntry(key []byte, members []ZSetMember, expireMS uint64) error {
	if err := e.writeExpire(expireMS); err != nil {
		return err
	}
	if err := e.w.writeByte(typeZSet); err != nil {
		return err
	}
	if err := e.w.writeString(key); err != nil {
		return err
	}
	if err := e.w.writeLen(uint64(len(members))); err != nil {
		return err
	}
	for _, m := range members {
		if err := e.w.writeString(m.Member); err != nil {
			return err
		}
		if err := e.w.writeRawString(formatScore(m.Score)); err != nil {
			return err
		}
	}
	return nil
}

// WriteStreamEntry writes a hayakv-internal stream entry.
// Format: [expire?] typeStream key
//
//	lastMs lastSeq maxDelMs maxDelSeq entriesAdded
//	numEntries
//	for each entry: ms seq numFields (fieldKey fieldValue)...
//	numGroups
//	for each group: groupName lastDelMs lastDelSeq entriesRead
//	  numPending
//	  for each pending: pMs pSeq consumer deliveryTime deliveryCount
//	  numConsumers
//	  for each consumer: consumerName activeTime
//	    numCPending
//	    for each cPending: pMs pSeq
func (e *Encoder) WriteStreamEntry(key []byte, s *stream.Stream, expireMS uint64) error {
	if err := e.writeExpire(expireMS); err != nil {
		return err
	}
	if err := e.w.writeByte(typeStream); err != nil {
		return err
	}
	if err := e.w.writeString(key); err != nil {
		return err
	}

	// Stream metadata
	if err := e.w.writeLen(s.LastID().Ms); err != nil {
		return err
	}
	if err := e.w.writeLen(s.LastID().Seq); err != nil {
		return err
	}
	if err := e.w.writeLen(s.MaxDeletedID().Ms); err != nil {
		return err
	}
	if err := e.w.writeLen(s.MaxDeletedID().Seq); err != nil {
		return err
	}
	if err := e.w.writeLen(s.EntriesAdded()); err != nil {
		return err
	}

	// Entries
	entries := s.Range(stream.StreamID{Ms: 0, Seq: 0}, stream.StreamID{Ms: ^uint64(0), Seq: ^uint64(0)}, -1)
	if err := e.w.writeLen(uint64(len(entries))); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := e.w.writeLen(entry.ID.Ms); err != nil {
			return err
		}
		if err := e.w.writeLen(entry.ID.Seq); err != nil {
			return err
		}
		if err := e.w.writeLen(uint64(len(entry.Fields))); err != nil {
			return err
		}
		for _, f := range entry.Fields {
			if err := e.w.writeString([]byte(f[0])); err != nil {
				return err
			}
			if err := e.w.writeString([]byte(f[1])); err != nil {
				return err
			}
		}
	}

	// Groups
	groups := s.Groups()
	if err := e.w.writeLen(uint64(len(groups))); err != nil {
		return err
	}
	for _, g := range groups {
		if err := e.w.writeString([]byte(g.Name)); err != nil {
			return err
		}
		if err := e.w.writeLen(g.LastDelivered().Ms); err != nil {
			return err
		}
		if err := e.w.writeLen(g.LastDelivered().Seq); err != nil {
			return err
		}
		if err := e.w.writeLen(g.EntriesRead()); err != nil {
			return err
		}

		// Pending entries (group-level PEL)
		pending := g.PendingMap()
		if err := e.w.writeLen(uint64(len(pending))); err != nil {
			return err
		}
		for id, pe := range pending {
			if err := e.w.writeLen(id.Ms); err != nil {
				return err
			}
			if err := e.w.writeLen(id.Seq); err != nil {
				return err
			}
			if err := e.w.writeString([]byte(pe.Consumer)); err != nil {
				return err
			}
			if err := e.w.writeLen(uint64(pe.DeliveryTime)); err != nil {
				return err
			}
			if err := e.w.writeLen(pe.DeliveryCount); err != nil {
				return err
			}
		}

		// Consumers
		consumers := g.Consumers()
		if err := e.w.writeLen(uint64(len(consumers))); err != nil {
			return err
		}
		for _, c := range consumers {
			if err := e.w.writeString([]byte(c.Name)); err != nil {
				return err
			}
			if err := e.w.writeLen(uint64(c.ActiveTime)); err != nil {
				return err
			}
			// Consumer pending list
			if err := e.w.writeLen(uint64(len(c.Pending))); err != nil {
				return err
			}
			for id := range c.Pending {
				if err := e.w.writeLen(id.Ms); err != nil {
					return err
				}
				if err := e.w.writeLen(id.Seq); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// WriteEnd writes 0xFF then the 8-byte little-endian CRC64 of everything so far.
func (e *Encoder) WriteEnd() error {
	if err := e.w.writeByte(opEOF); err != nil {
		return err
	}
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], e.w.crc) // crc already folds in the 0xFF byte
	_, err := e.w.w.Write(b[:])                  // trailer is NOT itself crc'd
	return err
}
