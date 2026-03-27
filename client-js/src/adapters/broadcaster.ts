// x402 SDK — WhatsOnChain broadcaster adapter.

import type { Broadcaster } from "../types.js"
import { BroadcastError } from "../errors.js"

export const WOC_MAINNET = "https://api.whatsonchain.com/v1/bsv/main"
export const WOC_TESTNET = "https://api.whatsonchain.com/v1/bsv/test"

export class WoCBroadcaster implements Broadcaster {
  private readonly baseUrl: string
  private readonly fetchFn: typeof globalThis.fetch

  constructor(network: "mainnet" | "testnet" | string, fetchFn?: typeof globalThis.fetch) {
    if (network === "mainnet") this.baseUrl = WOC_MAINNET
    else if (network === "testnet") this.baseUrl = WOC_TESTNET
    else this.baseUrl = network
    this.fetchFn = fetchFn ?? globalThis.fetch.bind(globalThis)
  }

  async broadcast(rawtxHex: string): Promise<string> {
    const res = await this.fetchFn(`${this.baseUrl}/tx/raw`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ txhex: rawtxHex }),
    })

    if (!res.ok) {
      const text = await res.text()
      throw new BroadcastError(`Broadcast failed (${res.status}): ${text}`, String(res.status))
    }

    const text = await res.text()
    const txid = text.replace(/^"|"$/g, "").trim()
    if (txid.length !== 64) {
      throw new BroadcastError(`Unexpected broadcast response: ${text}`)
    }
    return txid
  }
}
