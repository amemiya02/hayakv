package stream

import "testing"

func TestStreamAddRange(t *testing.T) {
	s := New()
	id1, _ := s.Add(StreamID{1, 0}, [][2]string{{"f", "v1"}})
	id2, _ := s.Add(StreamID{2, 0}, [][2]string{{"f", "v2"}})
	if !id2.Greater(id1) {
		t.Fatalf("ids not monotonic: %v %v", id1, id2)
	}
	if s.Len() != 2 {
		t.Fatalf("len = %d", s.Len())
	}
	e := s.Range(StreamID{0, 0}, StreamID{maxMs, maxSeq}, -1)
	if len(e) != 2 || e[0].ID != id1 {
		t.Fatalf("range wrong: %v", e)
	}
}

func TestStreamIDParse(t *testing.T) {
	id, auto, err := ParseID("1526919030474-55")
	if err != nil || id.Ms != 1526919030474 || id.Seq != 55 || auto {
		t.Fatalf("parse: %v %v %v", id, auto, err)
	}
	id2, auto2, err := ParseID("1526919030474")
	if err != nil || id2.Ms != 1526919030474 || id2.Seq != 0 || auto2 {
		t.Fatalf("parse ms-only: %v %v %v", id2, auto2, err)
	}
	id3, auto3, err := ParseID("5-*")
	if err != nil || id3.Ms != 5 || auto3 != true {
		t.Fatalf("parse ms-*: %v %v %v", id3, auto3, err)
	}
}

func TestStreamAddRejectsZeroID(t *testing.T) {
	s := New()
	_, err := s.Add(StreamID{0, 0}, [][2]string{{"f", "v"}})
	if err == nil {
		t.Fatal("Add(0-0) should be rejected")
	}
}

func TestStreamAddAutoAssign(t *testing.T) {
	s := New()
	id1, _ := s.AddAutoAssign(1000, [][2]string{{"f", "v1"}})
	id2, _ := s.AddAutoAssign(1000, [][2]string{{"f", "v2"}})
	if id1.Ms != 1000 || id1.Seq != 0 {
		t.Fatalf("auto-assign 1: %v", id1)
	}
	if id2.Ms != 1000 || id2.Seq != 1 {
		t.Fatalf("auto-assign 2: %v", id2)
	}
	id3, _ := s.AddAutoAssign(2000, [][2]string{{"f", "v3"}})
	if id3.Ms != 2000 || id3.Seq != 0 {
		t.Fatalf("auto-assign 3: %v", id3)
	}
}

func TestStreamDelete(t *testing.T) {
	s := New()
	s.Add(StreamID{1, 0}, [][2]string{{"f", "v1"}})
	s.Add(StreamID{2, 0}, [][2]string{{"f", "v2"}})
	s.Add(StreamID{3, 0}, [][2]string{{"f", "v3"}})
	deleted := s.Delete([]StreamID{{2, 0}})
	if deleted != 1 || s.Len() != 2 {
		t.Fatalf("delete: %d, len=%d", deleted, s.Len())
	}
	if s.MaxDeletedID().Ms != 2 {
		t.Fatalf("maxDeleted: %v", s.MaxDeletedID())
	}
}

func TestStreamTrimMaxLen(t *testing.T) {
	s := New()
	for i := uint64(1); i <= 10; i++ {
		s.Add(StreamID{i, 0}, [][2]string{{"f", "v"}})
	}
	removed := s.TrimMaxLen(5, false)
	if removed != 5 || s.Len() != 5 {
		t.Fatalf("trim: removed=%d, len=%d", removed, s.Len())
	}
}
