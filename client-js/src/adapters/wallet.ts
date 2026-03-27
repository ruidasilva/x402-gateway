// x402 SDK — Minimal wallet adapter (default: uses built-in tx builder).

import type { TransactionBuilder, Challenge } from "../types.js"
import { buildPartialTransaction } from "../transaction.js"

export class DefaultWallet implements TransactionBuilder {
  async buildPartial(challenge: Challenge): Promise<string> {
    return buildPartialTransaction(challenge)
  }
}
