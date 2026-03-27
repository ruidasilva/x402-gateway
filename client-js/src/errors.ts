// x402 SDK — Typed errors. No generic Error in public API.

export class ChallengeError extends Error {
  constructor(message: string) {
    super(message)
    this.name = "ChallengeError"
  }
}

export class ChallengeExpiredError extends ChallengeError {
  constructor(
    public readonly expiresAt: number,
    public readonly currentTime: number,
  ) {
    super(`Challenge expired at ${expiresAt} (current time: ${currentTime})`)
  }
}

export class DelegationError extends Error {
  readonly name = "DelegationError" as const
  constructor(
    message: string,
    public readonly status?: number,
    public readonly code?: string,
  ) {
    super(message)
  }
}

export class BroadcastError extends Error {
  readonly name = "BroadcastError" as const
  constructor(
    message: string,
    public readonly code?: string,
  ) {
    super(message)
  }
}

export class ProofRejectedError extends Error {
  readonly name = "ProofRejectedError" as const
  constructor(
    message: string,
    public readonly status: number,
    public readonly serverCode?: string,
    public readonly serverMessage?: string,
  ) {
    super(message)
  }
}

export class BindingMismatchError extends Error {
  readonly name = "BindingMismatchError" as const
  constructor(
    public readonly field: "method" | "path" | "query" | "domain",
    public readonly expected: string,
    public readonly actual: string,
  ) {
    super(`Binding mismatch on ${field}: expected "${expected}", got "${actual}"`)
  }
}
