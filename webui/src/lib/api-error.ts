export class APIError extends Error {
  readonly status: number
  readonly retryAfter: number | null

  constructor(message: string, status = 0, retryAfter: string | null = null) {
    super(message)
    this.name = "APIError"
    this.status = status
    const seconds = Number(retryAfter)
    this.retryAfter = retryAfter !== null && Number.isFinite(seconds) && seconds >= 0 ? seconds : null
  }
}
