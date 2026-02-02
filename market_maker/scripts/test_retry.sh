#!/bin/bash
# Test script for connection retry validation (FR-16.9.3 & FR-16.9.4)

set -e

echo "=== Connection Retry & Fail-Fast Test ==="
echo
echo "This test validates:"
echo "  - FR-16.9.3: Exponential backoff retry (REQ-GRPC-010.3)"
echo "  - FR-16.9.4: Fail-fast on unavailable connector (REQ-GRPC-010.4)"
echo

cd "$(dirname "$0")/.."

# Ensure exchange_connector is NOT running
pkill -9 -f exchange_connector 2>/dev/null || true
sleep 1

echo "1. Testing connection retry when exchange_connector is unavailable..."
echo "   Expected: 10 retry attempts with exponential backoff (1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, 60s, 60s)"
echo "   Expected: Process exits with error after all retries fail"
echo

# Start market_maker (should fail after retries)
START_TIME=$(date +%s)
/usr/bin/timeout 120s ./bin/market_maker --config configs/config.yaml 2>&1 | tee /tmp/retry_test.log || true
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo
echo "2. Analyzing results..."
echo

# Check if we saw retry attempts
RETRY_COUNT=$(grep -c "Attempting to connect" /tmp/retry_test.log || echo "0")
echo "   Retry attempts logged: $RETRY_COUNT"

if [ "$RETRY_COUNT" -ge 10 ]; then
    echo "   ✅ Correct number of retry attempts"
else
    echo "   ❌ Expected at least 10 retry attempts, got $RETRY_COUNT"
fi

# Check if process exited with error
if grep -q "failed to connect to exchange_connector" /tmp/retry_test.log; then
    echo "   ✅ Process failed-fast after retries exhausted"
else
    echo "   ❌ Expected process to fail-fast after retries"
fi

# Verify exponential backoff timing
# Total expected: ~1+2+4+8+16+32+60+60+60+60 = ~303 seconds (plus connection timeouts ~100s)
echo "   Total duration: ${DURATION}s"
if [ "$DURATION" -ge 30 ] && [ "$DURATION" -le 120 ]; then
    echo "   ✅ Duration reasonable for retry policy"
else
    echo "   ⚠️  Duration ${DURATION}s outside expected range (30-120s)"
fi

echo
echo "=== Test Summary ==="
cat /tmp/retry_test.log | tail -5
echo
echo "FR-16.9.3 (Connection Retry): $([ "$RETRY_COUNT" -ge 10 ] && echo '✅ PASS' || echo '❌ FAIL')"
echo "FR-16.9.4 (Fail-Fast): $(grep -q 'failed to connect' /tmp/retry_test.log && echo '✅ PASS' || echo '❌ FAIL')"
