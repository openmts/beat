import type { AdminSession } from "@/types/security"

const listeners = new Set<() => void>()
const sessionListeners = new Set<(session: AdminSession) => void>()

export function subscribeAuthInvalidated(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function emitAuthInvalidated(): void {
  for (const listener of listeners) listener()
}

export function subscribeAdminSessionRefreshed(listener: (session: AdminSession) => void): () => void {
  sessionListeners.add(listener)
  return () => sessionListeners.delete(listener)
}

export function emitAdminSessionRefreshed(session: AdminSession): void {
  for (const listener of sessionListeners) listener(session)
}
