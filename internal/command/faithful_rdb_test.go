package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

func TestFaithfulSaveAndReload(t *testing.T) {
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
	conn := connection.NewFakeConn()
	srv.Exec(conn, utils.ToCmdLine("SET", "k", "v"))
	srv.Exec(conn, utils.ToCmdLine("RPUSH", "l", "a", "b"))
	if r := SaveRDB(srv, nil); protocol.IsErrorReply(r) {
		t.Fatalf("SAVE: %s", r.ToBytes())
	}
	srv.Close()

	// fresh server loads the faithful dump on startup
	srv2 := NewStandaloneServer()
	defer srv2.Close()
	if r := srv2.Exec(conn, utils.ToCmdLine("GET", "k")); string(r.ToBytes()) != "$1\r\nv\r\n" {
		t.Fatalf("GET after reload = %q", r.ToBytes())
	}
	if r := srv2.Exec(conn, utils.ToCmdLine("LRANGE", "l", "0", "-1")); string(r.ToBytes()) != "*2\r\n$1\r\na\r\n$1\r\nb\r\n" {
		t.Fatalf("LRANGE after reload = %q", r.ToBytes())
	}
}
