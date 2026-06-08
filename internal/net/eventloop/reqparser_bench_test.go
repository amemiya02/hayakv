package eventloop

import "testing"

func BenchmarkParseRequestsPipeline100(b *testing.B) {
	var buf []byte
	for i := 0; i < 100; i++ {
		buf = append(buf, []byte("*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n")...)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = parseRequests(buf)
	}
}

func BenchmarkParseRequestsPipeline1(b *testing.B) {
	buf := []byte("*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = parseRequests(buf)
	}
}
