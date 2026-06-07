package database

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/persist/rdb"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestSnapshotIsPointInTime(t *testing.T) {
	tmp := t.TempDir()
	config.Properties = &config.ServerProperties{
		Databases:      16,
		Dir:            tmp,
		RDBFilename:    filepath.Join(tmp, "dump.rdb"),
		RdbImpl:        "faithful",
		AppendOnly:     true,
		AppendFilename: filepath.Join(tmp, "appendonly.aof"),
		AppendFsync:    "no",
	}
	_ = os.MkdirAll(config.GetTmpDir(), os.ModePerm)
	srv := NewStandaloneServer()
	defer srv.Close()
	conn := connection.NewFakeConn()
	srv.Exec(conn, utils.ToCmdLine("SET", "k", "before"))

	// take the snapshot, THEN mutate the live db
	snap := srv.snapshotAllDBs()
	srv.Exec(conn, utils.ToCmdLine("SET", "k", "after"))
	srv.Exec(conn, utils.ToCmdLine("SET", "k2", "new"))

	// encoding the snapshot must reflect the pre-mutation state only
	var buf bytes.Buffer
	if err := writeSnapshotRDB(snap, config.Properties.Databases, &buf); err != nil {
		t.Fatalf("writeSnapshotRDB: %v", err)
	}
	dec := rdb.NewDecoder(bytes.NewReader(buf.Bytes()))
	got := map[string][]byte{}
	_ = dec.Parse(func(e rdb.Entry) bool {
		if e.Type == rdb.EntryType(0) {
			got[string(e.Key)] = e.StringVal
		}
		return true
	})
	if !bytes.Equal(got["k"], []byte("before")) {
		t.Fatalf("snapshot k = %q, want before", got["k"])
	}
	if _, ok := got["k2"]; ok {
		t.Fatalf("snapshot must not contain k2 added after snapshot")
	}
}
