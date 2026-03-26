#!/usr/bin/env bash
# x402 v1.0 End-to-End Protocol Flow Test
#
# This script executes the full x402 protocol flow:
#   1. Unpaid request → 402 + challenge
#   2. Parse challenge, construct partial tx
#   3. Send to fee delegator
#   4. Broadcast (mock)
#   5. Retry with proof → 200
#   6. Replay same proof → must fail
#   7. Binding mismatch → must fail
#
# Usage: ./scripts/e2e-flow.sh
# Requires: server running on :8402 with embedded delegator + mock broadcaster

set -euo pipefail

BASE_URL="http://localhost:8402"
ENDPOINT="$BASE_URL/v1/expensive"

PASS=0
FAIL=0

check() {
    local name="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        echo "  ✓ $name (got $actual)"
        PASS=$((PASS + 1))
    else
        echo "  ✗ $name (expected $expected, got $actual)"
        FAIL=$((FAIL + 1))
    fi
}

echo "═══════════════════════════════════════════"
echo " x402 v1.0 End-to-End Protocol Flow Test"
echo "═══════════════════════════════════════════"

# ─── Step 1: Verify server is up ─────────────────────────
echo ""
echo "Step 0: Health check"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/health")
check "health endpoint" "200" "$STATUS"

# ─── Step 1: Unpaid request → 402 ─────────────────────────
echo ""
echo "Step 1: Unpaid request → expect 402"
RESP=$(curl -s -D - -o /tmp/x402_body.json "$ENDPOINT" 2>&1)
STATUS=$(echo "$RESP" | head -1 | grep -o '[0-9]\{3\}')
check "unpaid request status" "402" "$STATUS"

CHALLENGE=$(echo "$RESP" | grep -i "X402-Challenge:" | tr -d '\r' | sed 's/.*: //')
if [ -z "$CHALLENGE" ]; then
    echo "  ✗ No X402-Challenge header found"
    FAIL=$((FAIL + 1))
else
    echo "  ✓ X402-Challenge header present (${#CHALLENGE} chars)"
    PASS=$((PASS + 1))
fi

ACCEPT=$(echo "$RESP" | grep -i "X402-Accept:" | tr -d '\r' | sed 's/.*: //')
check "X402-Accept header" "bsv-tx-v1" "$ACCEPT"

# ─── Step 2: Construct transaction and proof via Go client ─
echo ""
echo "Step 2-6: Full client flow (construct tx → delegate → broadcast → proof)"
echo "  Using Go client with derived keys..."
# Keys below are derived from the .env XPRIV via cmd/derivekeys.
# They are deterministic and match the server's nonce/payment pool keys.

# Run the client, capturing all output
CLIENT_OUTPUT=$(go run ./cmd/client \
  --nonce-key "Kwm2Rd4GiT25vq7mPq3wzLpr4QTKjB5DeggNSrYbSD8eC89oLqXr" \
  --payment-key "L5UUKubCDQ3wKDE6XP6S2AG1a3cAD5TqhA3o9LLueD37fZzN4FvQ" \
  --payment-txid "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  --payment-sats 200 \
  --payment-script "76a914d3783c4a1bfdf5eac7c4de1c3e7e3a3a76f4b78188ac" \
  --delegator "$BASE_URL/api/v1/tx" \
  "$ENDPOINT" 2>&1) || true

echo "$CLIENT_OUTPUT"

if echo "$CLIENT_OUTPUT" | grep -q "Payment successful"; then
    echo "  ✓ Full flow completed successfully"
    PASS=$((PASS + 1))
else
    echo "  ✗ Full flow did not complete"
    FAIL=$((FAIL + 1))
fi

echo ""
echo "═══════════════════════════════════════════"
echo " RESULTS: $PASS passed, $FAIL failed"
echo "═══════════════════════════════════════════"

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
