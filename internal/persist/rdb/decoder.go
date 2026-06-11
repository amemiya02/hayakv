package rdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
)

// Decoder parses an RDB v11 stream into Entry values.
type Decoder struct {
	r *reader
}

// NewDecoder returns a Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: newReader(r)} }

// Parse walks the stream, calling fn for each key/value Entry. It validates the
// header magic and the trailing CRC64. Returning false from fn stops early.
func (d *Decoder) Parse(fn func(Entry) bool) error {
	header, err := d.r.readFull(9)
	if err != nil {
		return fmt.Errorf("rdb: read header: %w", err)
	}
	if !bytes.HasPrefix(header, []byte("REDIS")) {
		return fmt.Errorf("rdb: bad magic %q", header[:5])
	}
	ver, err := strconv.Atoi(string(header[5:9]))
	if err != nil || ver < 1 || ver > Version {
		return fmt.Errorf("rdb: unsupported version %q", header[5:9])
	}

	curDB := 0
	var pendingExpireMS uint64
	for {
		op, err := d.r.readByte()
		if err != nil {
			return fmt.Errorf("rdb: read opcode: %w", err)
		}
		switch op {
		case opEOF:
			return d.verifyCRC()
		case opSelectDB:
			n, _, err := d.r.readLen()
			if err != nil {
				return err
			}
			curDB = int(n)
		case opResizeDB:
			if _, _, err := d.r.readLen(); err != nil { // db size hint
				return err
			}
			if _, _, err := d.r.readLen(); err != nil { // expires size hint
				return err
			}
		case opAux:
			if _, err := d.r.readString(); err != nil { // aux key
				return err
			}
			if _, err := d.r.readString(); err != nil { // aux value
				return err
			}
		case opExpireMS:
			b, err := d.r.readFull(8)
			if err != nil {
				return err
			}
			pendingExpireMS = binary.LittleEndian.Uint64(b)
		case opExpireSec:
			b, err := d.r.readFull(4)
			if err != nil {
				return err
			}
			pendingExpireMS = uint64(binary.LittleEndian.Uint32(b)) * 1000
		case opIdle:
			if _, _, err := d.r.readLen(); err != nil { // LRU idle seconds
				return err
			}
		case opFreq:
			if _, err := d.r.readByte(); err != nil { // LFU counter
				return err
			}
		default:
			entry, err := d.readObject(op, curDB, pendingExpireMS)
			if err != nil {
				return err
			}
			pendingExpireMS = 0
			if !fn(entry) {
				return nil
			}
		}
	}
}

func (d *Decoder) readObject(typeByte byte, db int, expireMS uint64) (Entry, error) {
	key, err := d.r.readString()
	if err != nil {
		return Entry{}, err
	}
	e := Entry{DBIndex: db, Key: key, Type: EntryType(typeByte), ExpireMS: expireMS}
	switch typeByte {
	case typeString:
		e.StringVal, err = d.r.readString()
		return e, err
	case typeList:
		vals, err := d.readStringVector()
		e.ListVal = vals
		return e, err
	case typeSet:
		vals, err := d.readStringVector()
		e.SetVal = vals
		return e, err
	case typeHash:
		n, _, err := d.r.readLen()
		if err != nil {
			return e, err
		}
		e.HashVal = make(map[string][]byte, n)
		for i := uint64(0); i < n; i++ {
			f, err := d.r.readString()
			if err != nil {
				return e, err
			}
			v, err := d.r.readString()
			if err != nil {
				return e, err
			}
			e.HashVal[string(f)] = v
		}
		return e, nil
	case typeZSet:
		n, _, err := d.r.readLen()
		if err != nil {
			return e, err
		}
		e.ZSetVal = make([]ZSetMember, 0, n)
		for i := uint64(0); i < n; i++ {
			m, err := d.r.readString()
			if err != nil {
				return e, err
			}
			scoreStr, err := d.r.readString()
			if err != nil {
				return e, err
			}
			score, err := strconv.ParseFloat(string(scoreStr), 64)
			if err != nil {
				return e, fmt.Errorf("rdb: bad zset score %q: %w", scoreStr, err)
			}
			e.ZSetVal = append(e.ZSetVal, ZSetMember{Member: m, Score: score})
		}
		return e, nil
	case typeStream:
		sd, err := d.readStreamData()
		if err != nil {
			return e, err
		}
		e.StreamVal = sd
		return e, nil
	default:
		return e, fmt.Errorf("rdb: unsupported value type %d (listpack/intset/quicklist variants are not supported)", typeByte)
	}
}

func (d *Decoder) readStreamData() (*StreamData, error) {
	// Stream metadata
	lastMs, _, err := d.r.readLen()
	if err != nil {
		return nil, err
	}
	lastSeq, _, err := d.r.readLen()
	if err != nil {
		return nil, err
	}
	maxDelMs, _, err := d.r.readLen()
	if err != nil {
		return nil, err
	}
	maxDelSeq, _, err := d.r.readLen()
	if err != nil {
		return nil, err
	}
	entriesAdded, _, err := d.r.readLen()
	if err != nil {
		return nil, err
	}

	// Entries
	numEntries, _, err := d.r.readLen()
	if err != nil {
		return nil, err
	}
	entries := make([]StreamEntry, numEntries)
	for i := uint64(0); i < numEntries; i++ {
		ms, _, err := d.r.readLen()
		if err != nil {
			return nil, err
		}
		seq, _, err := d.r.readLen()
		if err != nil {
			return nil, err
		}
		numFields, _, err := d.r.readLen()
		if err != nil {
			return nil, err
		}
		fields := make([][2]string, numFields)
		for j := uint64(0); j < numFields; j++ {
			fk, err := d.r.readString()
			if err != nil {
				return nil, err
			}
			fv, err := d.r.readString()
			if err != nil {
				return nil, err
			}
			fields[j] = [2]string{string(fk), string(fv)}
		}
		entries[i] = StreamEntry{
			ID:     StreamID{Ms: ms, Seq: seq},
			Fields: fields,
		}
	}

	// Groups
	numGroups, _, err := d.r.readLen()
	if err != nil {
		return nil, err
	}
	groups := make([]StreamGroupData, numGroups)
	for i := uint64(0); i < numGroups; i++ {
		gName, err := d.r.readString()
		if err != nil {
			return nil, err
		}
		gLastMs, _, err := d.r.readLen()
		if err != nil {
			return nil, err
		}
		gLastSeq, _, err := d.r.readLen()
		if err != nil {
			return nil, err
		}
		gEntriesRead, _, err := d.r.readLen()
		if err != nil {
			return nil, err
		}

		// Pending entries
		numPending, _, err := d.r.readLen()
		if err != nil {
			return nil, err
		}
		pending := make([]StreamPendingEntry, numPending)
		for j := uint64(0); j < numPending; j++ {
			pMs, _, err := d.r.readLen()
			if err != nil {
				return nil, err
			}
			pSeq, _, err := d.r.readLen()
			if err != nil {
				return nil, err
			}
			pConsumer, err := d.r.readString()
			if err != nil {
				return nil, err
			}
			pTime, _, err := d.r.readLen()
			if err != nil {
				return nil, err
			}
			pCount, _, err := d.r.readLen()
			if err != nil {
				return nil, err
			}
			pending[j] = StreamPendingEntry{
				ID:            StreamID{Ms: pMs, Seq: pSeq},
				Consumer:      string(pConsumer),
				DeliveryTime:  int64(pTime),
				DeliveryCount: pCount,
			}
		}

		// Consumers
		numConsumers, _, err := d.r.readLen()
		if err != nil {
			return nil, err
		}
		consumers := make([]StreamConsumerData, numConsumers)
		for j := uint64(0); j < numConsumers; j++ {
			cName, err := d.r.readString()
			if err != nil {
				return nil, err
			}
			cActive, _, err := d.r.readLen()
			if err != nil {
				return nil, err
			}
			cNumPending, _, err := d.r.readLen()
			if err != nil {
				return nil, err
			}
			cPending := make([]StreamID, cNumPending)
			for k := uint64(0); k < cNumPending; k++ {
				cpMs, _, err := d.r.readLen()
				if err != nil {
					return nil, err
				}
				cpSeq, _, err := d.r.readLen()
				if err != nil {
					return nil, err
				}
				cPending[k] = StreamID{Ms: cpMs, Seq: cpSeq}
			}
			consumers[j] = StreamConsumerData{
				Name:       string(cName),
				ActiveTime: int64(cActive),
				Pending:    cPending,
			}
		}

		groups[i] = StreamGroupData{
			Name:          string(gName),
			LastDelivered: StreamID{Ms: gLastMs, Seq: gLastSeq},
			EntriesRead:   gEntriesRead,
			Pending:       pending,
			Consumers:     consumers,
		}
	}

	return &StreamData{
		LastID:       StreamID{Ms: lastMs, Seq: lastSeq},
		MaxDeletedID: StreamID{Ms: maxDelMs, Seq: maxDelSeq},
		EntriesAdded: entriesAdded,
		Entries:      entries,
		Groups:       groups,
	}, nil
}

func (d *Decoder) readStringVector() ([][]byte, error) {
	n, _, err := d.r.readLen()
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, n)
	for i := uint64(0); i < n; i++ {
		s, err := d.r.readString()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// verifyCRC reads the 8-byte trailer and compares it to the running crc. A zero
// trailer means the writer disabled checksums and is accepted as-is.
func (d *Decoder) verifyCRC() error {
	expected := d.r.crc // crc already includes the 0xFF EOF byte
	var trailer [8]byte
	if _, err := io.ReadFull(d.r.r, trailer[:]); err != nil {
		return fmt.Errorf("rdb: read crc trailer: %w", err)
	}
	stored := binary.LittleEndian.Uint64(trailer[:])
	if stored == 0 {
		return nil // checksum disabled
	}
	if stored != expected {
		return fmt.Errorf("rdb: crc mismatch stored=%#016x computed=%#016x", stored, expected)
	}
	return nil
}

// readDouble reads an 8-byte little-endian IEEE-754 double (RDB_TYPE_ZSET_2
// binary score, matching Redis rdbLoadBinaryDoubleValue).
func (d *Decoder) readDouble() (float64, error) {
	b, err := d.r.readFull(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
}
