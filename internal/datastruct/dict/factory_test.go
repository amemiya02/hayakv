package dict

import "testing"

func TestSetEngine(t *testing.T) {
	original := GetEngine()
	defer SetEngine(original)

	SetEngine(EngineShardMap)
	if GetEngine() != EngineShardMap {
		t.Errorf("expected %s, got %s", EngineShardMap, GetEngine())
	}
	SetEngine(EngineRedisDB)
	if GetEngine() != EngineRedisDB {
		t.Errorf("expected %s, got %s", EngineRedisDB, GetEngine())
	}
}

func TestMakeDict(t *testing.T) {
	d := MakeDict()
	if d == nil {
		t.Fatal("MakeDict returned nil")
	}
	if d.Len() != 0 {
		t.Errorf("expected len 0, got %d", d.Len())
	}
	d.Put("k", "v")
	val, ok := d.Get("k")
	if !ok || val != "v" {
		t.Errorf("expected v, got %v", val)
	}
}

func TestMakeRedisDict(t *testing.T) {
	d := MakeRedisDict(0)
	if d == nil {
		t.Fatal("MakeRedisDict returned nil")
	}
	// Verify it satisfies Dict interface
	var _ Dict = d
	if d.Len() != 0 {
		t.Errorf("expected len 0, got %d", d.Len())
	}
	d.Put("k", "v")
	val, ok := d.Get("k")
	if !ok || val != "v" {
		t.Errorf("expected v, got %v", val)
	}
}

func TestSetDictSize(t *testing.T) {
	original := dictSize
	defer func() { dictSize = original }()

	SetDictSize(1024)
	if dictSize != 1024 {
		t.Errorf("expected 1024, got %d", dictSize)
	}
}
