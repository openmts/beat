import { useState } from "react"
import { deleteTheme } from "@/lib/theme-api"

export function useThemeDeletion() {
  const [pending, setPending] = useState<string[]>([])
  const remove = async (id: string) => {
    const result = await deleteTheme(id)
    setPending((current) => result?.cleanup_pending ? [...new Set([...current, id])] : current.filter((value) => value !== id))
  }
  return { pending, remove }
}
