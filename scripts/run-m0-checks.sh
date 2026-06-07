#!/usr/bin/env bash
set -euo pipefail

go vet ./...
go test ./... -run TestDoesNotExist -count=0
go test -race ./config ./internal/datastruct/... ./internal/proto/... ./internal/net/goroutine ./internal/net/eventloop ./internal/server ./internal/iface ./internal/command ./cmd/hayakv -count=1
go test ./test/integration -count=1
go test ./test/integration -run 'TestReplica|TestReplconf|TestWait|TestDiskless|TestPartial|TestFullResync|TestLive|TestPromote|TestInfoReplication' -count=1 -p 1
go test ./test/diff -run TestM0DifferentialRESP2 -count=1
go test ./test/diff -run TestM2DifferentialRedisDB -count=1
go build ./cmd/hayakv
