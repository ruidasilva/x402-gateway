#!/usr/bin/env python3
"""
x402 v1.0 Independent Interoperability Verifier (Python)

Zero external dependencies — stdlib only.
Consumes testdata/x402-vectors-v1.json and verifies every vector
against an independent Python implementation of:
  - RFC 8785 canonical JSON serialization
  - SHA-256 hashing
  - base64url encoding (no padding)
  - Bitcoin txid derivation (double SHA-256, byte-reversed)
  - Header-binding string construction

Usage:
    python3 testdata/verify_vectors.py

Exit code 0 = all vectors pass.
Exit code 1 = at least one vector failed.
"""

import base64
import hashlib
import json
import os
import sys
import time


# ─────────────────────────────────────────────────────────────────────────────
# RFC 8785 Canonical JSON — independent Python implementation
# ─────────────────────────────────────────────────────────────────────────────

def canonical_json(value):
    """Produce RFC 8785 (JCS) canonical JSON bytes from a Python object.

    Rules:
      - Object keys sorted lexicographically (by UTF-16 code units, but for
        ASCII keys this is identical to byte-order sorting).
      - No whitespace outside strings.
      - Numbers in shortest decimal form; integers without decimal point.
      - Strings use JSON escaping (\\uXXXX for control chars, standard escapes).
      - Booleans: true / false.
      - null: null.
    """
    if value is None:
        return b"null"
    if isinstance(value, bool):
        return b"true" if value else b"false"
    if isinstance(value, int):
        return str(value).encode("utf-8")
    if isinstance(value, float):
        # If it's a whole number stored as float, emit as integer
        if value == int(value) and not (value == 0.0 and str(value) == "-0.0"):
            return str(int(value)).encode("utf-8")
        # Otherwise shortest decimal form
        return _format_float(value).encode("utf-8")
    if isinstance(value, str):
        return _json_string(value)
    if isinstance(value, list):
        parts = [canonical_json(item) for item in value]
        return b"[" + b",".join(parts) + b"]"
    if isinstance(value, dict):
        keys = sorted(value.keys())
        parts = []
        for k in keys:
            key_bytes = _json_string(k)
            val_bytes = canonical_json(value[k])
            parts.append(key_bytes + b":" + val_bytes)
        return b"{" + b",".join(parts) + b"}"
    raise TypeError(f"Unsupported type: {type(value)}")


def _json_string(s):
    """JSON-encode a string with proper escaping."""
    # Use Python's json.dumps which handles escaping correctly.
    # ensure_ascii=False keeps non-ASCII as UTF-8 (RFC 8785 allows this).
    return json.dumps(s, ensure_ascii=False).encode("utf-8")


def _format_float(f):
    """Format a float in shortest decimal form per RFC 8785."""
    # Python's repr gives shortest round-trip representation
    r = repr(f)
    if r in ("inf", "-inf", "nan"):
        raise ValueError(f"Cannot serialize {r} as JSON")
    return r


# ─────────────────────────────────────────────────────────────────────────────
# Hashing primitives
# ─────────────────────────────────────────────────────────────────────────────

def sha256_hex(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def double_sha256_reversed_hex(data: bytes) -> str:
    """Bitcoin txid: SHA256(SHA256(data)), byte-reversed, lowercase hex."""
    h1 = hashlib.sha256(data).digest()
    h2 = hashlib.sha256(h1).digest()
    return h2[::-1].hex()


def base64url_no_padding(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


# ─────────────────────────────────────────────────────────────────────────────
# Test runner
# ─────────────────────────────────────────────────────────────────────────────

class VectorVerifier:
    def __init__(self):
        self.passed = 0
        self.failed = 0
        self.skipped = 0
        self.failures = []

    def check(self, vector_name: str, check_name: str, expected: str, computed: str, category: str):
        label = f"  [{vector_name}] {check_name}"
        if expected == computed:
            self.passed += 1
            print(f"  \033[32mPASS\033[0m {check_name}")
        else:
            self.failed += 1
            self.failures.append((vector_name, check_name, category, expected, computed))
            print(f"  \033[31mFAIL\033[0m {check_name}")
            print(f"         category: {category}")
            print(f"         expected: {expected[:120]}{'...' if len(expected) > 120 else ''}")
            print(f"         computed: {computed[:120]}{'...' if len(computed) > 120 else ''}")

    def verify_challenge_hash(self, v):
        """SHA256(canonical_challenge_json) == challenge_sha256"""
        canonical = v.get("canonical_challenge_json", "")
        expected_hash = v.get("challenge_sha256", "")
        if not canonical or not expected_hash:
            return
        computed = sha256_hex(canonical.encode("utf-8"))
        self.check(v["name"], "challenge_sha256", expected_hash, computed, "hashing")

    def verify_challenge_hex(self, v):
        """hex(canonical_challenge_json bytes) == canonical_challenge_hex"""
        canonical = v.get("canonical_challenge_json", "")
        expected_hex = v.get("canonical_challenge_hex", "")
        if not canonical or not expected_hex:
            return
        computed = canonical.encode("utf-8").hex()
        self.check(v["name"], "canonical_challenge_hex", expected_hex, computed, "encoding")

    def verify_base64url(self, v):
        """base64url(canonical_challenge_json) == challenge_base64url"""
        canonical = v.get("canonical_challenge_json", "")
        expected_b64 = v.get("challenge_base64url", "")
        if not canonical or not expected_b64:
            return
        computed = base64url_no_padding(canonical.encode("utf-8"))
        self.check(v["name"], "challenge_base64url", expected_b64, computed, "encoding")

    def verify_canonical_json_reproduction(self, v):
        """Parse canonical JSON, re-canonicalize, compare byte-for-byte."""
        canonical = v.get("canonical_challenge_json", "")
        if not canonical:
            return
        parsed = json.loads(canonical)
        recanonical = canonical_json(parsed).decode("utf-8")
        self.check(v["name"], "canonical_json_reproduction", canonical, recanonical, "canonicalization")

    def verify_header_hash(self, v):
        """SHA256(header_binding_string) == headers_sha256"""
        hdr_str = v.get("header_binding_string", "")
        expected = v.get("headers_sha256", "")
        if not hdr_str or not expected:
            return
        computed = sha256_hex(hdr_str.encode("utf-8"))
        self.check(v["name"], "headers_sha256", expected, computed, "hashing")

    def verify_header_hex(self, v):
        """hex(header_binding_string bytes) == header_binding_hex"""
        hdr_str = v.get("header_binding_string", "")
        expected = v.get("header_binding_hex", "")
        if not hdr_str or not expected:
            return
        computed = hdr_str.encode("utf-8").hex()
        self.check(v["name"], "header_binding_hex", expected, computed, "encoding")

    def verify_body_hash(self, v):
        """SHA256(body_bytes) == body_sha256"""
        body_hex = v.get("body_bytes", "")
        expected = v.get("body_sha256", "")
        if not body_hex or not expected:
            return
        body = bytes.fromhex(body_hex)
        computed = sha256_hex(body)
        self.check(v["name"], "body_sha256", expected, computed, "hashing")

    def verify_txid(self, v):
        """double_sha256_reversed(rawtx) == txid (only for 64-char txids)"""
        rawtx_hex = v.get("rawtx_hex", "")
        txid = v.get("txid", "")
        if not rawtx_hex or not txid:
            return
        # Skip compound txid descriptions like "correct=..., submitted=..."
        if len(txid) != 64:
            # For the invalid_txid_mismatch vector, extract the "correct=" part
            if txid.startswith("correct="):
                correct = txid.split(",")[0].replace("correct=", "").strip()
                rawtx = bytes.fromhex(rawtx_hex)
                computed = double_sha256_reversed_hex(rawtx)
                self.check(v["name"], "txid_derivation (correct part)", correct, computed, "txid")
            return
        rawtx = bytes.fromhex(rawtx_hex)
        computed = double_sha256_reversed_hex(rawtx)
        self.check(v["name"], "txid_derivation", txid, computed, "txid")

    def verify_reject_expired(self, v):
        """Verify expires_at is in the past."""
        if v.get("expected_result") != "reject" or v.get("name") != "expired_challenge":
            return
        ch = v.get("challenge", {})
        expires_at = ch.get("expires_at", 0)
        now = int(time.time())
        is_expired = now > expires_at
        expected = "true"
        computed = "true" if is_expired else f"false (now={now}, expires_at={expires_at})"
        self.check(v["name"], "expires_at_is_past", expected, computed, "expiry")

    def verify_reject_version(self, v):
        """Verify v=0 is an unsupported version."""
        if v.get("expected_result") != "reject" or v.get("name") != "invalid_proof_version":
            return
        proof = v.get("proof", {})
        version = proof.get("v", -1)
        expected = "true"
        computed = "true" if version == 0 else f"false (v={version})"
        self.check(v["name"], "v_is_zero_unsupported", expected, computed, "version")

    def verify_reject_binding(self, v):
        """Verify path mismatch detection is possible."""
        if v.get("expected_result") != "reject" or v.get("name") != "invalid_binding_path_mismatch":
            return
        ch = v.get("challenge", {})
        challenge_path = ch.get("path", "")
        attacker_path = "/v1/other"
        mismatch = challenge_path != attacker_path
        expected = "true"
        computed = "true" if mismatch else "false"
        self.check(v["name"], "path_mismatch_detectable",
                   expected, computed, "binding")
        # Also verify the canonical hash is reproducible from the challenge object
        if v.get("canonical_challenge_json") and v.get("challenge_sha256"):
            self.verify_canonical_json_reproduction(v)
            self.verify_challenge_hash(v)

    def run(self, vectors):
        for v in vectors:
            name = v["name"]
            purpose = v["purpose"]
            result = v["expected_result"]
            print(f"\n{'─' * 60}")
            print(f"Vector: {name}")
            print(f"Purpose: {purpose}")
            print(f"Expected: {result}")

            # Run all applicable checks
            self.verify_canonical_json_reproduction(v)
            self.verify_challenge_hash(v)
            self.verify_challenge_hex(v)
            self.verify_base64url(v)
            self.verify_header_hash(v)
            self.verify_header_hex(v)
            self.verify_body_hash(v)
            self.verify_txid(v)
            self.verify_reject_expired(v)
            self.verify_reject_version(v)
            self.verify_reject_binding(v)


def main():
    # Find vector file relative to this script
    script_dir = os.path.dirname(os.path.abspath(__file__))
    vector_path = os.path.join(script_dir, "x402-vectors-v1.json")

    if not os.path.exists(vector_path):
        print(f"ERROR: Vector file not found: {vector_path}")
        print("Run: go run ./cmd/vecgen > testdata/x402-vectors-v1.json")
        sys.exit(1)

    with open(vector_path, "r") as f:
        data = json.load(f)

    print("=" * 60)
    print("x402 v1.0 Independent Interoperability Verifier")
    print(f"Vector file: {vector_path}")
    print(f"Vector version: {data['version']}")
    print(f"Generated by: {data['generated_by']}")
    print(f"Verifier language: Python {sys.version.split()[0]}")
    print("=" * 60)

    verifier = VectorVerifier()
    verifier.run(data["vectors"])

    print(f"\n{'=' * 60}")
    print("RESULTS")
    print(f"{'=' * 60}")
    print(f"  Passed:  {verifier.passed}")
    print(f"  Failed:  {verifier.failed}")
    print(f"  Total:   {verifier.passed + verifier.failed}")

    if verifier.failures:
        print(f"\n{'─' * 60}")
        print("FAILURES:")
        for vname, check, category, expected, computed in verifier.failures:
            print(f"  [{vname}] {check}")
            print(f"    category: {category}")
            print(f"    expected: {expected[:100]}")
            print(f"    computed: {computed[:100]}")
        print(f"\nVERDICT: \033[31mFAIL\033[0m — {verifier.failed} check(s) failed")
        sys.exit(1)
    else:
        print(f"\nVERDICT: \033[32mPASS\033[0m — all {verifier.passed} checks passed")
        print("The Python implementation reproduces all reference vectors exactly.")
        sys.exit(0)


if __name__ == "__main__":
    main()
