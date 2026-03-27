// x402 SDK — HTTP delegator adapter.

import type { Delegator, DelegationInput, CompletedTransaction } from "../types.js"
import { DelegationError } from "../errors.js"

export class HttpDelegator implements Delegator {
  private readonly url: string
  private readonly fetchFn: typeof globalThis.fetch

  constructor(baseUrl: string, path = "/delegate/x402", fetchFn?: typeof globalThis.fetch) {
    this.url = baseUrl.replace(/\/$/, "") + path
    this.fetchFn = fetchFn ?? globalThis.fetch.bind(globalThis)
  }

  async complete(input: DelegationInput): Promise<CompletedTransaction> {
    const res = await this.fetchFn(this.url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ partial_tx: input.partialTxHex }),
    })

    if (!res.ok) {
      let code: string | undefined
      try {
        const body = await res.json()
        code = body.error ?? body.code
      } catch { /* non-JSON error */ }
      throw new DelegationError(
        `Delegator returned ${res.status}`,
        res.status,
        code,
      )
    }

    const data = await res.json()
    const rawtxHex = data.completed_tx ?? data.rawtx_hex
    const txid = data.txid

    if (!rawtxHex || !txid) {
      throw new DelegationError("Delegator response missing completed_tx or txid")
    }

    return { txid, rawtxHex }
  }
}
