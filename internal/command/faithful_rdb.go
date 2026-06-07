package database

import (
	"fmt"
	"os"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/persist/aof"
	"github.com/amemiya02/hayakv/internal/persist/rdb"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

// saveFaithfulRDB writes all databases to rdbFilename using the own RDB v11
// codec. It writes to a temp file in the same dir then renames for atomicity.
func (server *Server) saveFaithfulRDB(rdbFilename string) error {
	tmp, err := os.CreateTemp(config.GetTmpDir(), "*.rdb")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := aof.DumpEngineToRDB(server, config.Properties.Databases, tmp); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, rdbFilename)
}

// loadFaithfulRDB reads rdbFilename with the own decoder and replays each entry
// as write commands (so indexing + aof shadowing run exactly like AOF replay).
func (server *Server) loadFaithfulRDB(rdbFilename string) error {
	f, err := os.Open(rdbFilename)
	if err != nil {
		return fmt.Errorf("open rdb: %w", err)
	}
	defer func() { _ = f.Close() }()

	fakeConn := connection.NewFakeConn()
	dec := rdb.NewDecoder(f)
	return dec.Parse(func(e rdb.Entry) bool {
		fakeConn.SelectDB(e.DBIndex)
		for _, batch := range aof.LoadEntriesAsCommands(e) {
			for _, cmd := range batch {
				server.Exec(fakeConn, cmd)
			}
		}
		return true
	})
}
