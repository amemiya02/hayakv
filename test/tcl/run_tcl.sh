#!/bin/bash

# TCL runner scaffold for hayakv
# This script runs TCL tests against the hayakv server

set -e

# Configuration
HAYAKV_PORT=${HAYAKV_PORT:-6379}
HAYAKV_HOST=${HAYAKV_HOST:-127.0.0.1}
TCL_TEST_DIR=${TCL_TEST_DIR:-./tests}
TCL_SERVER=${TCL_SERVER:-redis-server}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check if server is running
check_server() {
    if redis-cli -h $HAYAKV_HOST -p $HAYAKV_PORT ping > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Function to wait for server
wait_for_server() {
    local max_attempts=30
    local attempt=1

    print_info "Waiting for server at $HAYAKV_HOST:$HAYAKV_PORT..."

    while [ $attempt -le $max_attempts ]; do
        if check_server; then
            print_info "Server is ready!"
            return 0
        fi

        print_warn "Attempt $attempt/$max_attempts: Server not ready, waiting..."
        sleep 1
        attempt=$((attempt + 1))
    done

    print_error "Server did not become ready within $max_attempts seconds"
    return 1
}

# Function to run TCL tests
run_tcl_tests() {
    local test_file=$1

    if [ ! -f "$test_file" ]; then
        print_error "Test file not found: $test_file"
        return 1
    fi

    print_info "Running TCL tests from: $test_file"

    # Run the TCL test
    tclsh $test_file $HAYAKV_HOST $HAYAKV_PORT
}

# Function to show usage
show_usage() {
    echo "Usage: $0 [OPTIONS] [TEST_FILE]"
    echo ""
    echo "Options:"
    echo "  -h, --help          Show this help message"
    echo "  -p, --port PORT     Set server port (default: 6379)"
    echo "  -H, --host HOST     Set server host (default: 127.0.0.1)"
    echo "  -d, --dir DIR       Set test directory (default: ./tests)"
    echo ""
    echo "Examples:"
    echo "  $0                          # Run all tests in ./tests"
    echo "  $0 tests/object.tcl         # Run specific test file"
    echo "  $0 -p 6380 tests/object.tcl # Run on different port"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_usage
            exit 0
            ;;
        -p|--port)
            HAYAKV_PORT="$2"
            shift 2
            ;;
        -H|--host)
            HAYAKV_HOST="$2"
            shift 2
            ;;
        -d|--dir)
            TCL_TEST_DIR="$2"
            shift 2
            ;;
        *)
            TEST_FILE="$1"
            shift
            ;;
    esac
done

# Main execution
print_info "Starting TCL test runner for hayakv"
print_info "Server: $HAYAKV_HOST:$HAYAKV_PORT"

# Check if server is running
if ! check_server; then
    print_error "Server is not running at $HAYAKV_HOST:$HAYAKV_PORT"
    print_info "Please start the hayakv server first"
    exit 1
fi

# Run tests
if [ -n "$TEST_FILE" ]; then
    # Run specific test file
    run_tcl_tests "$TEST_FILE"
else
    # Run all tests in directory
    if [ ! -d "$TCL_TEST_DIR" ]; then
        print_warn "Test directory not found: $TCL_TEST_DIR"
        print_info "Creating empty test directory..."
        mkdir -p "$TCL_TEST_DIR"
    fi

    # Find and run all .tcl files
    test_count=0
    pass_count=0
    fail_count=0

    for test_file in "$TCL_TEST_DIR"/*.tcl; do
        if [ -f "$test_file" ]; then
            test_count=$((test_count + 1))
            print_info "Running: $test_file"

            if run_tcl_tests "$test_file"; then
                pass_count=$((pass_count + 1))
                print_info "PASSED: $test_file"
            else
                fail_count=$((fail_count + 1))
                print_error "FAILED: $test_file"
            fi
        fi
    done

    # Print summary
    echo ""
    print_info "Test Summary:"
    print_info "  Total:  $test_count"
    print_info "  Passed: $pass_count"
    print_info "  Failed: $fail_count"

    if [ $fail_count -gt 0 ]; then
        exit 1
    fi
fi

print_info "TCL test runner completed"
