// x402 SDK — Proof construction.

import type { Proof, RequestBinding, RequestContext, CompletedTransaction, Challenge } from "./types.js"
import { hashHeaders, hashBody } from "./challenge.js"
import { BindingMismatchError } from "./errors.js"

export function buildRequestBinding(request: RequestContext): RequestBinding {
  return {
    method: request.method.toUpperCase(),
    path: request.url.pathname,
    query: request.url.search.replace(/^\?/, ""),
    req_headers_sha256: hashHeaders(request.headers),
    req_body_sha256: hashBody(request.body),
  }
}

export function buildProof(params: {
  tx: CompletedTransaction
  challengeHash: string
  challenge: Challenge
  request: RequestContext
}): { proof: Proof; header: string } {
  // Pre-flight binding check
  const req = params.request
  const ch = params.challenge

  if (req.method.toUpperCase() !== ch.method) {
    throw new BindingMismatchError("method", ch.method, req.method.toUpperCase())
  }
  if (req.url.pathname !== ch.path) {
    throw new BindingMismatchError("path", ch.path, req.url.pathname)
  }
  const reqQuery = req.url.search.replace(/^\?/, "")
  if (reqQuery !== ch.query) {
    throw new BindingMismatchError("query", ch.query, reqQuery)
  }
  if (req.url.host !== ch.domain) {
    throw new BindingMismatchError("domain", ch.domain, req.url.host)
  }

  const binding = buildRequestBinding(req)

  const proof: Proof = {
    v: 1,
    scheme: "bsv-tx-v1",
    challenge_sha256: params.challengeHash,
    request: binding,
    payment: {
      txid: params.tx.txid,
      rawtx_b64: Buffer.from(params.tx.rawtxHex, "hex").toString("base64"),
    },
  }

  const header = Buffer.from(JSON.stringify(proof), "utf-8").toString("base64url")
  return { proof, header }
}

export function encodeProofHeader(proof: Proof): string {
  return Buffer.from(JSON.stringify(proof), "utf-8").toString("base64url")
}
