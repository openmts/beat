import { APIError } from "@/lib/api-error"
export function operationMutationError(error: unknown, conflict: string): unknown {
 return error instanceof APIError && error.status === 409 ? new Error(conflict) : error
}
