#!/usr/bin/env bash
# Productized TCL test runner for hayakv.
# Clones the pinned redis/redis tag, builds hayakv, creates a redis-server shim,
# and runs the upstream TCL test suite against hayakv via test_helper.tcl.
#
# Usage:
#   bash test/tcl/run_tcl.sh                         # run all in-scope files
#   bash test/tcl/run_tcl.sh tests/unit/type/string.tcl  # run one file
#
# Environment:
#   REDIS_GIT_TAG   – redis/redis tag to check out (default: parsed from redisversion.go)
#   REDIS_TCL_DIR   – path to an already-checked-out redis/tests dir (skip clone)
#   TCL_SHIM_PORT   – port the shim server listens on (default: 6379)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# ---------------------------------------------------------------------------
# Resolve the pinned redis git tag from redisversion.go
# ---------------------------------------------------------------------------
resolve_tag() {
    grep -oE '"[0-9]+\.[0-9]+\.[0-9]+"' "$SCRIPT_DIR/redisversion.go" \
        | head -1 | tr -d '"'
}

REDIS_GIT_TAG="${REDIS_GIT_TAG:-$(resolve_tag)}"
TCL_SHIM_PORT="${TCL_SHIM_PORT:-6379}"

# ---------------------------------------------------------------------------
# Locate or clone redis tests
# ---------------------------------------------------------------------------
if [[ -n "${REDIS_TCL_DIR:-}" && -d "$REDIS_TCL_DIR" ]]; then
    REDIS_TESTS_DIR="$REDIS_TCL_DIR"
else
    TMPDIR_ROOT="${TMPDIR:-/tmp}/hayakv-tcl"
    mkdir -p "$TMPDIR_ROOT"
    REDIS_SRC="$TMPDIR_ROOT/redis-$REDIS_GIT_TAG"

    if [[ ! -d "$REDIS_SRC/tests" ]]; then
        echo "[tcl] cloning redis/redis@$REDIS_GIT_TAG into $REDIS_SRC ..."
        git clone --depth 1 --branch "$REDIS_GIT_TAG" \
            https://github.com/redis/redis "$REDIS_SRC" 2>&1
    fi
    REDIS_TESTS_DIR="$REDIS_SRC/tests"
fi

echo "[tcl] redis tests dir: $REDIS_TESTS_DIR"
echo "[tcl] redis git tag:   $REDIS_GIT_TAG"

# ---------------------------------------------------------------------------
# Build hayakv binary
# ---------------------------------------------------------------------------
HAYAKV_BIN="$TMPDIR_ROOT/hayakv"
echo "[tcl] building hayakv -> $HAYAKV_BIN ..."
go build -o "$HAYAKV_BIN" "$REPO_ROOT/cmd/hayakv"

# ---------------------------------------------------------------------------
# Create redis-server shim that execs hayakv
# ---------------------------------------------------------------------------
SHIM_DIR="$TMPDIR_ROOT/shim"
mkdir -p "$SHIM_DIR"
cat > "$SHIM_DIR/redis-server" <<SHIM
#!/usr/bin/env bash
# Shim: the TCL suite invokes "redis-server [args...]"; we translate to hayakv.
# hayakv accepts the same --port / --loglevel flags as redis-server.
exec "$HAYAKV_BIN" "\$@"
SHIM
chmod +x "$SHIM_DIR/redis-server"

# Also provide a redis-cli shim if the system doesn't have one.
if ! command -v redis-cli &>/dev/null; then
    cat > "$SHIM_DIR/redis-cli" <<'CLISHIM'
#!/usr/bin/env bash
# Minimal redis-cli shim: connect to localhost:$port and relay stdin/stdout.
# Only handles the basic case the TCL suite needs (PING, SET, GET, etc.)
exec socat - TCP:127.0.0.1:${1:-6379} 2>/dev/null || {
    echo "redis-cli shim: socat not available" >&2
    exit 1
}
CLISHIM
    chmod +x "$SHIM_DIR/redis-cli"
fi

# Prepend shim dir so "redis-server" resolves to our hayakv wrapper.
export PATH="$SHIM_DIR:$PATH"

# ---------------------------------------------------------------------------
# Determine which test files to run
# ---------------------------------------------------------------------------
if [[ $# -gt 0 ]]; then
    # Explicit file list from the caller
    TEST_FILES=("$@")
else
    # Default: run the in-scope files listed in manifest.yaml
    TEST_FILES=()
    while IFS= read -r line; do
        # Extract file paths where status is "pass" or "partial"
        file=$(echo "$line" | sed -n 's/.*file:\s*"\?\([^"]*\)"\?.*/\1/p')
        status=$(echo "$line" | sed -n 's/.*status:\s*"\?\([^"]*\)"\?.*/\1/p')
        if [[ -n "$file" && ("$status" == "pass" || "$status" == "partial") ]]; then
            TEST_FILES+=("$file")
        fi
    done < "$SCRIPT_DIR/manifest.yaml"
fi

if [[ ${#TEST_FILES[@]} -eq 0 ]]; then
    echo "[tcl] no in-scope test files found in manifest.yaml"
    exit 0
fi

echo "[tcl] running ${#TEST_FILES[@]} test file(s) ..."

# ---------------------------------------------------------------------------
# Run each file through test_helper.tcl and collect results
# ---------------------------------------------------------------------------
PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
SUMMARY_FILE="$TMPDIR_ROOT/summary.tsv"
: > "$SUMMARY_FILE"

for tf in "${TEST_FILES[@]}"; do
    echo -n "[tcl] $tf ... "
    LOG="$TMPDIR_ROOT/$(echo "$tf" | tr '/' '_').log"

    if tclsh "$REDIS_TESTS_DIR/test_helper.tcl" \
        --single "$tf" \
        --port "$TCL_SHIM_PORT" \
        > "$LOG" 2>&1; then
        # Extract pass/fail counts from the log if available
        passed=$(grep -oE 'passed [0-9]+' "$LOG" | tail -1 | grep -oE '[0-9]+' || echo "?")
        failed=$(grep -oE 'failed [0-9]+' "$LOG" | tail -1 | grep -oE '[0-9]+' || echo "?")
        echo "PASS (passed=$passed failed=$failed)"
        printf "%s\t%s\t%s\t%s\n" "$tf" "pass" "$passed" "$failed" >> "$SUMMARY_FILE"
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        passed=$(grep -oE 'passed [0-9]+' "$LOG" | tail -1 | grep -oE '[0-9]+' || echo "?")
        failed=$(grep -oE 'failed [0-9]+' "$LOG" | tail -1 | grep -oE '[0-9]+' || echo "?")
        echo "FAIL (passed=$passed failed=$failed)"
        printf "%s\t%s\t%s\t%s\n" "$tf" "fail" "$passed" "$failed" >> "$SUMMARY_FILE"
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
done

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "========================================"
echo " TCL Test Summary"
echo "========================================"
echo " Passed:  $PASS_COUNT"
echo " Failed:  $FAIL_COUNT"
echo " Total:   ${#TEST_FILES[@]}"
echo "========================================"
echo ""
echo "Per-file results (file<TAB>status<TAB>passed<TAB>failed):"
cat "$SUMMARY_FILE"

if [[ $FAIL_COUNT -gt 0 ]]; then
    exit 1
fi
