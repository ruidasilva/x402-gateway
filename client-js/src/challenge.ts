// x402 SDK — Challenge parsing, canonical JSON, hashing.

import { createHash } from "node:crypto"
import type { Challenge, ParsedChallenge } from "./types.js"
import { ChallengeError, ChallengeExpiredError } from "./errors.js"

export const CHALLENGE_HEADER = "x402-challenge"
export const PROOF_HEADER = "X402-Proof"

const HEADER_ALLOWLIST = [
  "accept",
  "content-length",
  "content-type",
  "x402-client",
  "x402-idempotency-key",
]

export function parseChallenge(headerValue: string): ParsedChallenge {
  let payload = headerValue
  const prefixMatch = payload.match(/^v\d+\.[^.]+\.(.+)$/)
  if (prefixMatch) payload = prefixMatch[1]

  let rawBytes: Buffer
  try {
    rawBytes = Buffer.from(payload, "base64url")
  } catch {
    throw new ChallengeError("Invalid base64url in X402-Challenge header")
  }

  let parsed: Record<string, unknown>
  try {
    parsed = JSON.parse(rawBytes.toString("utf-8"))
  } catch {
    throw new ChallengeError("Invalid JSON in X402-Challenge header")
  }

  if (parsed.v !== 1) {
    throw new ChallengeError(`Unsupported challenge version: ${parsed.v} (expected 1)`)
  }
  if (parsed.scheme !== "bsv-tx-v1") {
    throw new ChallengeError(`Unsupported scheme: ${parsed.scheme} (expected "bsv-tx-v1")`)
  }
  if (!parsed.nonce_utxo || typeof parsed.nonce_utxo !== "object") {
    throw new ChallengeError("Challenge missing nonce_utxo")
  }
  if (!parsed.payee_locking_script_hex) {
    throw new ChallengeError("Challenge missing payee_locking_script_hex")
  }
  if (typeof parsed.amount_sats !== "number" || parsed.amount_sats <= 0) {
    throw new ChallengeError("Challenge missing or invalid amount_sats")
  }

  const canonicalBytes = Buffer.from(canonicalize(parsed), "utf-8")
  const challengeHash = sha256hex(canonicalBytes)

  return {
    challenge: parsed as unknown as Challenge,
    canonicalBytes: new Uint8Array(canonicalBytes),
    challengeHash,
  }
}

export function assertNotExpired(challenge: Challenge): void {
  const now = Math.floor(Date.now() / 1000)
  if (challenge.expires_at > 0 && now > challenge.expires_at) {
    throw new ChallengeExpiredError(challenge.expires_at, now)
  }
}

// RFC 8785 canonical JSON: sorted keys, no whitespace, shortest numbers.
export function canonicalize(value: unknown): string {
  if (value === null || value === undefined) return "null"
  if (typeof value === "boolean") return value ? "true" : "false"
  if (typeof value === "number") {
    if (!isFinite(value)) return "null"
    return Object.is(value, -0) ? "0" : String(value)
  }
  if (typeof value === "string") return JSON.stringify(value)
  if (Array.isArray(value)) {
    return "[" + value.map(canonicalize).join(",") + "]"
  }
  if (typeof value === "object") {
    const obj = value as Record<string, unknown>
    const keys = Object.keys(obj).sort()
    const pairs: string[] = []
    for (const k of keys) {
      if (obj[k] === undefined) continue
      pairs.push(JSON.stringify(k) + ":" + canonicalize(obj[k]))
    }
    return "{" + pairs.join(",") + "}"
  }
  return String(value)
}

export function sha256hex(data: Buffer | Uint8Array | string): string {
  return createHash("sha256")
    .update(typeof data === "string" ? Buffer.from(data, "utf-8") : data)
    .digest("hex")
}

export function hashHeaders(headers: Headers | Record<string, string>): string {
  const sorted = [...HEADER_ALLOWLIST].sort()
  const parts: string[] = []

  for (const key of sorted) {
    let value = ""
    if (headers instanceof Headers) {
      value = headers.get(key) ?? ""
    } else {
      for (const [k, v] of Object.entries(headers)) {
        if (k.toLowerCase() === key) { value = v; break }
      }
    }
    parts.push(`${key}:${value.trim()}`)
  }

  let canonical = parts.join("\n")
  if (parts.length > 0) canonical += "\n"
  return sha256hex(canonical)
}

export function hashBody(body: string | Uint8Array | null | undefined): string {
  if (!body || (typeof body === "string" && body.length === 0)) return sha256hex("")
  if (typeof body === "string") return sha256hex(body)
  return sha256hex(Buffer.from(body))
}
