package aof

import (
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	m := &Manifest{
		Files: []ManifestEntry{
			{FileName: "appendonly.aof.1.base.rdb", Seq: 1, Type: AOFManifestTypeBase},
			{FileName: "appendonly.aof.1.incr.aof", Seq: 1, Type: AOFManifestTypeIncr},
		},
	}
	text := m.Serialize()
	want := "file appendonly.aof.1.base.rdb seq 1 type b\nfile appendonly.aof.1.incr.aof seq 1 type i\n"
	if text != want {
		t.Fatalf("serialize:\n got %q\nwant %q", text, want)
	}
	parsed, err := ParseManifest([]byte(text))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Files) != 2 {
		t.Fatalf("got %d files, want 2", len(parsed.Files))
	}
	if parsed.Files[0].Type != AOFManifestTypeBase || parsed.Files[0].Seq != 1 {
		t.Fatalf("base entry wrong: %+v", parsed.Files[0])
	}
	if parsed.Files[1].FileName != "appendonly.aof.1.incr.aof" {
		t.Fatalf("incr entry wrong: %+v", parsed.Files[1])
	}
}

func TestManifestAccessors(t *testing.T) {
	m, err := ParseManifest([]byte("file b.rdb seq 3 type b\nfile i.aof seq 7 type i\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Base() == nil || m.Base().FileName != "b.rdb" {
		t.Fatalf("Base() wrong: %+v", m.Base())
	}
	incrs := m.Incrs()
	if len(incrs) != 1 || incrs[0].Seq != 7 {
		t.Fatalf("Incrs() wrong: %+v", incrs)
	}
}

func TestParseManifestMalformed(t *testing.T) {
	cases := []string{
		"file only-three tokens\n",
		"file x.aof seq notnum type i\n",
		"file x.aof seq 1 type z\n",
		"notfile x.aof seq 1 type i\n",
	}
	for _, c := range cases {
		if _, err := ParseManifest([]byte(c)); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}
