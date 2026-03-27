// x402 SDK — Dev/CI utility: verify canonical test vectors.
// NOT used at runtime. Validates that this SDK's canonicalization,
// hashing, and encoding match the frozen reference vectors.

import { readFileSync } from "node:fs"
import { canonicalize, sha256hex } from "../challenge.js"

interface VectorFile {
  version: string
  vectors: Vector[]
}

interface Vector {
  name: string
  expected_result: string
  canonical_challenge_json?: string
  canonical_challenge_hex?: string
  challenge_sha256?: string
  challenge_base64url?: string
  header_binding_string?: string
  header_binding_hex?: string
  headers_sha256?: string
  body_sha256?: string
  body_bytes?: string
  rawtx_hex?: string
  txid?: string
}

export async function verifyVectorFile(path: string): Promise<void> {
  const raw = readFileSync(path, "utf-8")
  const data: VectorFile = JSON.parse(raw)
  const errors: string[] = []

  for (const v of data.vectors) {
    // Challenge hash
    if (v.canonical_challenge_json && v.challenge_sha256) {
      const computed = sha256hex(v.canonical_challenge_json)
      if (computed !== v.challenge_sha256) {
        errors.push(`[${v.name}] challenge_sha256: expected ${v.challenge_sha256}, got ${computed}`)
      }
    }

    // Challenge hex
    if (v.canonical_challenge_json && v.canonical_challenge_hex) {
      const computed = Buffer.from(v.canonical_challenge_json, "utf-8").toString("hex")
      if (computed !== v.canonical_challenge_hex) {
        errors.push(`[${v.name}] canonical_challenge_hex mismatch`)
      }
    }

    // Base64url
    if (v.canonical_challenge_json && v.challenge_base64url) {
      const computed = Buffer.from(v.canonical_challenge_json, "utf-8").toString("base64url")
      if (computed !== v.challenge_base64url) {
        errors.push(`[${v.name}] challenge_base64url mismatch`)
      }
    }

    // Header hash
    if (v.header_binding_string && v.headers_sha256) {
      const computed = sha256hex(v.header_binding_string)
      if (computed !== v.headers_sha256) {
        errors.push(`[${v.name}] headers_sha256: expected ${v.headers_sha256}, got ${computed}`)
      }
    }

    // Body hash
    if (v.body_bytes && v.body_sha256) {
      const body = Buffer.from(v.body_bytes, "hex")
      const computed = sha256hex(body)
      if (computed !== v.body_sha256) {
        errors.push(`[${v.name}] body_sha256: expected ${v.body_sha256}, got ${computed}`)
      }
    }

    // Txid derivation
    if (v.rawtx_hex && v.txid && v.txid.length === 64) {
      const { createHash } = await import("node:crypto")
      const raw = Buffer.from(v.rawtx_hex, "hex")
      const h1 = createHash("sha256").update(raw).digest()
      const h2 = createHash("sha256").update(h1).digest()
      const computed = Buffer.from(h2).reverse().toString("hex")
      if (computed !== v.txid) {
        errors.push(`[${v.name}] txid: expected ${v.txid}, got ${computed}`)
      }
    }

    // Canonical JSON reproduction
    if (v.canonical_challenge_json) {
      const parsed = JSON.parse(v.canonical_challenge_json)
      const reproduced = canonicalize(parsed)
      if (reproduced !== v.canonical_challenge_json) {
        errors.push(`[${v.name}] canonical JSON reproduction mismatch`)
      }
    }
  }

  if (errors.length > 0) {
    throw new Error(`Vector verification failed:\n${errors.join("\n")}`)
  }
}
