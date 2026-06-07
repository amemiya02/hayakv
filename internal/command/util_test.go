package database

import (
	"github.com/amemiya02/hayakv/internal/datastruct/dict"
)

func makeTestDB() *DB {
	return &DB{
		data:       dict.MakeConcurrent(dataDictSize),
		versionMap: dict.MakeConcurrent(dataDictSize),
		ttlMap:     dict.MakeConcurrent(ttlDictSize),
		lruMap:     dict.MakeConcurrent(dataDictSize),
		persister:  func(line CmdLine) {},
	}
}
