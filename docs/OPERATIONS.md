# Operations Guide

## Deploy

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/x402-server ./cmd/server
scp bin/x402-server <host>:~/x402/bin/x402-server
ssh <host> 'sudo systemctl restart x402-gateway.service'
```

## Verify Deployment

After restart, confirm:

```bash
# Check config loaded correctly
sudo journalctl -u x402-gateway.service --since "30 seconds ago" | grep "config loaded"
# Expected: fee_rate=0.001

# Check version
curl -s http://localhost:8402/version | python3 -m json.tool

# Run E2E test
node examples/pay-for-weather.mjs https://demo.x402.merkleworks.io
```

## What to Check in Logs

After each delegation, the log line `delegation accepted` contains:

| Field | Expected | Problem if wrong |
|---|---|---|
| `fee_inputs` | 1 | >1 means fee UTXOs are too small or fee_rate too high |
| `fee_input_sats` | 100 | Should match FEE_UTXO_SATS |
| `miner_fee_est` | 1 | >1 at standard rate means tx is unexpectedly large |
| `change_sats` | 0 | >0 is valid (change returned) but indicates excess |
| `output_sats` | 100 | Should match template price_sats |

## Common Failure Modes

### Wrong FEE_RATE

**Symptom:** `fee_inputs=2`, `miner_fee_est=52`, `change_sats=0`

**Cause:** `FEE_RATE=0.1` instead of `FEE_RATE=0.001`. The `.env` was
edited but the service was not restarted.

**Fix:**
```bash
# Verify .env
grep FEE_RATE ~/x402/.env
# Must be: FEE_RATE=0.001

# Restart
sudo systemctl restart x402-gateway.service

# Verify
sudo journalctl -u x402-gateway.service --since "10 seconds ago" | grep fee_rate
```

### Broadcaster Issues

**Symptom:** Broadcast returns error or tx not visible in mempool.

**Check:**
```bash
sudo journalctl -u x402-gateway.service | grep -i "broadcast\|error"
```

**Common causes:**
- WoC rate limiting (429) -- composite broadcaster falls back to ARC
- ARC fee policy rejection -- check fee_rate is correct
- Network connectivity

### No Nonce UTXOs Available

**Symptom:** `503 no_utxos_available`

**Cause:** All nonce UTXOs are leased or spent.

**Fix:** Check pool stats at `/health`. If `available=0` and `leased`
is high, wait for lease expiry (reclaim loop runs every 2 minutes).
If `spent` equals `total`, the pool needs re-seeding via treasury
fan-out.

## Safe Restart

The gateway is stateless for protocol correctness. Restarting is safe:

1. In-flight challenges expire naturally (TTL-based)
2. Leased nonces are reclaimed after lease expiry
3. Replay protection derives from UTXO single-spend, not server state
4. Redis-backed pools survive restart; in-memory pools re-seed from chain

```bash
sudo systemctl restart x402-gateway.service
```

No data loss occurs. Clients with unexpired challenges can retry.
Clients with expired challenges receive a new 402 challenge.
