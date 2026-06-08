package stream

import "testing"

func TestGroupCreateAndPEL(t *testing.T) {
	s := New()
	id, _ := s.Add(StreamID{1, 0}, [][2]string{{"f", "v"}})
	g, err := s.CreateGroup("g1", StreamID{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	d := g.ReadNew(s, "consumer-A", 10)
	if len(d) != 1 || d[0].ID != id {
		t.Fatalf("ReadNew = %v", d)
	}
	if g.PendingCount() != 1 {
		t.Fatalf("PEL = %d", g.PendingCount())
	}
	if g.Ack([]StreamID{id}) != 1 {
		t.Fatal("Ack")
	}
	if g.PendingCount() != 0 {
		t.Fatalf("PEL after ack = %d", g.PendingCount())
	}
}
