#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="${HAYAKV_GODIS_SRC:-/tmp/hayakv-godis-src}"
GODIS_REPO="${HAYAKV_GODIS_REPO:-https://github.com/HDT3213/godis.git}"

cd "$ROOT"

if [[ -e go.mod || -e cmd || -e internal ]]; then
  echo "refusing to import: go.mod, cmd/, or internal/ already exists" >&2
  exit 1
fi

rm -rf "$SRC"
git clone --depth=1 "$GODIS_REPO" "$SRC"

rsync -a \
  --exclude '.git' \
  --exclude '.github' \
  --exclude '.vscode' \
  --exclude '.travis.yml' \
  --exclude 'godis' \
  --exclude 'logs' \
  "$SRC"/ "$ROOT"/

rm -f build-all.sh build-darwin.sh build-linux.sh

mkdir -p \
  cmd/hayakv \
  internal/iface \
  internal/net/goroutine/tcp \
  internal/proto/resp2 \
  internal/server \
  internal/persist

mv main.go cmd/hayakv/main.go

mv interface/database internal/iface/database
mv interface/redis internal/iface/redis
mv interface/tcp internal/iface/tcp
rmdir interface

mv redis/connection internal/server/connection
mv redis/client internal/client
mv redis/parser internal/proto/resp2/parser
mv redis/protocol internal/proto/resp2/protocol
mv redis/server/std/* internal/net/goroutine/
mv redis/server/gnet internal/net/gnet
rmdir redis/server/std redis/server redis

mv tcp/* internal/net/goroutine/tcp/
rmdir tcp

mv database internal/command
mv datastruct internal/datastruct
mv aof internal/persist/aof
mv pubsub internal/pubsub
mv cluster internal/cluster
mv lib internal/lib

find . -name '*.go' -type f -print0 | xargs -0 perl -0pi -e '
  s#github\.com/hdt3213/godis/interface/database#github.com/amemiya02/hayakv/internal/iface/database#g;
  s#github\.com/hdt3213/godis/interface/redis#github.com/amemiya02/hayakv/internal/iface/redis#g;
  s#github\.com/hdt3213/godis/interface/tcp#github.com/amemiya02/hayakv/internal/iface/tcp#g;
  s#github\.com/hdt3213/godis/redis/connection#github.com/amemiya02/hayakv/internal/server/connection#g;
  s#github\.com/hdt3213/godis/redis/client#github.com/amemiya02/hayakv/internal/client#g;
  s#github\.com/hdt3213/godis/redis/parser#github.com/amemiya02/hayakv/internal/proto/resp2/parser#g;
  s#github\.com/hdt3213/godis/redis/protocol#github.com/amemiya02/hayakv/internal/proto/resp2/protocol#g;
  s#github\.com/hdt3213/godis/redis/server/std#github.com/amemiya02/hayakv/internal/net/goroutine#g;
  s#github\.com/hdt3213/godis/redis/server/gnet#github.com/amemiya02/hayakv/internal/net/gnet#g;
  s#github\.com/hdt3213/godis/tcp#github.com/amemiya02/hayakv/internal/net/goroutine/tcp#g;
  s#github\.com/hdt3213/godis/database#github.com/amemiya02/hayakv/internal/command#g;
  s#github\.com/hdt3213/godis/datastruct#github.com/amemiya02/hayakv/internal/datastruct#g;
  s#github\.com/hdt3213/godis/aof#github.com/amemiya02/hayakv/internal/persist/aof#g;
  s#github\.com/hdt3213/godis/pubsub#github.com/amemiya02/hayakv/internal/pubsub#g;
  s#github\.com/hdt3213/godis/cluster#github.com/amemiya02/hayakv/internal/cluster#g;
  s#github\.com/hdt3213/godis/config#github.com/amemiya02/hayakv/config#g;
  s#github\.com/hdt3213/godis/lib#github.com/amemiya02/hayakv/internal/lib#g;
'

perl -0pi -e 's/module github\.com\/hdt3213\/godis/module github.com\/amemiya02\/hayakv/' go.mod

cat > NOTICE <<'NOTICE'
hayakv contains source code derived from HDT3213/godis:
https://github.com/HDT3213/godis

The imported godis code is licensed under GPL-3.0. hayakv is distributed
under GPL-3.0 and preserves the upstream LICENSE file.
NOTICE

gofmt -w $(find . -name '*.go' -type f)
go mod tidy

echo "godis import complete"
