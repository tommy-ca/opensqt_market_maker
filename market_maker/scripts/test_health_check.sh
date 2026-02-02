#!/bin/bash
# Test script for exchange_connector health check validation
# This demonstrates FR-16.9.1 implementation (REQ-GRPC-001.4)

set -e

echo "=== Exchange Connector Health Check Test ==="
echo

# Start exchange_connector in background
echo "1. Starting exchange_connector (binance mock mode)..."
cd "$(dirname "$0")/.."
EXCHANGE=binance API_KEY=test API_SECRET=test ./bin/exchange_connector &
CONNECTOR_PID=$!

# Give it time to start
sleep 3

# Trap to cleanup on exit
cleanup() {
    echo
    echo "Cleaning up..."
    if [ ! -z "$CONNECTOR_PID" ]; then
        kill $CONNECTOR_PID 2>/dev/null || true
    fi
}
trap cleanup EXIT

# Test 1: Overall health check
echo "2. Testing overall health check..."
if ./bin/grpc_health_probe -addr=localhost:50051; then
    echo "   ✅ Overall health check PASSED"
else
    echo "   ❌ Overall health check FAILED"
    exit 1
fi

echo

# Test 2: ExchangeService-specific health check
echo "3. Testing ExchangeService health check..."
if ./bin/grpc_health_probe -addr=localhost:50051 -service=opensqt.market_maker.v1.ExchangeService; then
    echo "   ✅ ExchangeService health check PASSED"
else
    echo "   ❌ ExchangeService health check FAILED"
    exit 1
fi

echo
echo "=== All Health Check Tests PASSED ==="
echo
echo "FR-16.9.1 (REQ-GRPC-001.4) Implementation: ✅ COMPLETE"
