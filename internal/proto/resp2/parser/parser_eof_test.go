package parser

import (
	"bytes"
	"testing"

	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func TestParseDisklessEOFRDB(t *testing.T) {
	mark := "0123456789abcdef0123456789abcdef01234567" // 40 bytes
	rdbBytes := []byte("REDIS0011\xff\x00\x00\x00\x00\x00\x00\x00\x00")
	var stream bytes.Buffer
	stream.WriteString("+FULLRESYNC abc 0\r\n")
	stream.WriteString("$EOF:" + mark + "\r\n")
	stream.Write(rdbBytes)
	stream.WriteString(mark)
	stream.Write(protocol.MakeMultiBulkReply([][]byte{[]byte("PING")}).ToBytes())

	ch := ParseStream(bytes.NewReader(stream.Bytes()))

	p := <-ch
	if p.Err != nil {
		t.Fatalf("status err: %v", p.Err)
	}
	if _, ok := p.Data.(*protocol.StatusReply); !ok {
		t.Fatalf("first payload not status: %T", p.Data)
	}
	p = <-ch
	if p.Err != nil {
		t.Fatalf("rdb err: %v", p.Err)
	}
	bulk, ok := p.Data.(*protocol.BulkReply)
	if !ok {
		t.Fatalf("second payload not bulk: %T", p.Data)
	}
	if !bytes.Equal(bulk.Arg, rdbBytes) {
		t.Fatalf("rdb bytes = %q, want %q", bulk.Arg, rdbBytes)
	}
	p = <-ch
	if p.Err != nil {
		t.Fatalf("ping err: %v", p.Err)
	}
	if _, ok := p.Data.(*protocol.MultiBulkReply); !ok {
		t.Fatalf("third payload not multibulk: %T", p.Data)
	}
}
