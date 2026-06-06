#!/usr/bin/env bash
set -euo pipefail

go vet ./...
go test ./... -run TestDoesNotExist -count=0
go test -race ./config ./internal/datastruct/... ./internal/proto/resp2/... ./internal/net/goroutine ./internal/server ./internal/iface ./cmd/hayakv -count=1
go test ./test/integration -count=1
go test ./test/diff -count=1
go build ./cmd/hayakv
