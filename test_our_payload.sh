#!/bin/bash
#
# Test script for OUR custom unified payload application
# Tests our custom client against the library's server in both modes
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
TEST_DIR="/tmp/our_payload_test"
SERVER_DIR="/var/bkgdata/go-fdo-merge/examples/cmd"
CLIENT_DIR="/home/windsurf/CascadeProjects/windsurf-project-4"
SERVER_ADDR="127.0.0.1:9998"  # Different port to avoid conflicts
SERVER_URL="http://${SERVER_ADDR}"
SERVER_PID=""

# Cleanup function
cleanup() {
    if [ -n "$SERVER_PID" ]; then
        echo -e "${YELLOW}Stopping server (PID: $SERVER_PID)${NC}"
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
}

# Set trap for cleanup
trap cleanup EXIT

# Helper functions
log_step() {
    echo -e "${BLUE}>>> $1${NC}"
}

log_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

log_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Start server function
start_server() {
    local flags="$1"
    log_step "Initializing server database and keys"
    cd "$SERVER_DIR"
    ./fdo server -initOnly -db "$TEST_DIR/test.db" > "$TEST_DIR/server_init.log" 2>&1
    
    log_step "Starting library server with flags: $flags"
    ./fdo server -http "$SERVER_ADDR" $flags > "$TEST_DIR/server.log" 2>&1 &
    SERVER_PID=$!
    sleep 2  # Give server time to start
    
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        log_error "Server failed to start"
        cat "$TEST_DIR/server.log"
        exit 1
    fi
    log_success "Server started (PID: $SERVER_PID)"
}

# Test our client
test_our_client() {
    local test_name="$1"
    log_step "Testing OUR client: $test_name"
    cd "$CLIENT_DIR"
    
    # Create temporary config with server URL
    local temp_config="$TEST_DIR/temp_config.yaml"
    cp config.yaml "$temp_config"
    sed -i "s|url: \"\"|url: \"$SERVER_URL\"|" "$temp_config"
    
    # Step 1: Perform DI first
    log_step "Performing Device Initialization"
    timeout 10s ./fdo-client -config "$temp_config" -di > "$TEST_DIR/client_di.log" 2>&1
    
    # Step 2: Perform TO1/TO2
    log_step "Performing TO1/TO2"
    timeout 20s ./fdo-client -config "$temp_config" > "$TEST_DIR/client.log" 2>&1
    
    log_success "Our client completed"
}

# Verify payload
verify_payload() {
    local original="$1"
    local received="$2"
    
    if [ ! -f "$received" ]; then
        log_error "Received file not found: $received"
        return 1
    fi
    
    original_hash=$(sha256sum "$original" | awk '{print $1}')
    received_hash=$(sha256sum "$received" | awk '{print $1}')
    
    if [ "$original_hash" = "$received_hash" ]; then
        log_success "File hashes match! Payload transferred correctly"
        log_success "  Original:  $original_hash"
        log_success "  Received:  $received_hash"
        return 0
    else
        log_error "File hashes don't match!"
        log_error "  Original:  $original_hash"
        log_error "  Received:  $received_hash"
        return 1
    fi
}

# Main test function
test_our_unified_payload() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}TEST: Our Unified Payload System${NC}"
    echo -e "${BLUE}========================================${NC}"
    
    # Setup
    mkdir -p "$TEST_DIR"
    rm -f "$TEST_DIR"/*
    
    # Create test payload
    local payload_file="$TEST_DIR/test_payload.bin"
    local received_file="$TEST_DIR/test_payload.bin"
    log_step "Creating test payload"
    dd if=/dev/urandom of="$payload_file" bs=1024 count=5 2>/dev/null
    log_success "Created test payload: $payload_file"
    
    # Test 1: Non-acked mode (-payload-file)
    echo -e "${YELLOW}--- Test 1: Non-acked payload mode ---${NC}"
    start_server "-payload-file $payload_file -payload-mime application/octet-stream -db $TEST_DIR/test.db"
    test_our_client "Non-acked mode"
    cleanup
    
    if verify_payload "$payload_file" "$received_file"; then
        log_success "✓ Non-acked payload test PASSED"
    else
        log_error "✗ Non-acked payload test FAILED"
        return 1
    fi
    
    # Clean up for next test
    rm -f "$TEST_DIR"/*
    
    # Test 2: RequireAck mode (-payload)
    echo -e "${YELLOW}--- Test 2: RequireAck payload mode ---${NC}"
    start_server "-payload application/octet-stream:$payload_file -db $TEST_DIR/test.db"
    test_our_client "RequireAck mode"
    cleanup
    
    if verify_payload "$payload_file" "$received_file"; then
        log_success "✓ RequireAck payload test PASSED"
    else
        log_error "✗ RequireAck payload test FAILED"
        return 1
    fi
    
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}🎉 OUR UNIFIED PAYLOAD SYSTEM TEST PASSED!${NC}"
    echo -e "${GREEN}========================================${NC}"
    
    # Show logs for debugging
    echo -e "${BLUE}Server log:${NC}"
    cat "$TEST_DIR/server.log" | tail -10
    echo -e "${BLUE}Client log:${NC}"
    cat "$TEST_DIR/client.log" | tail -10
}

# Run the test
test_our_unified_payload
