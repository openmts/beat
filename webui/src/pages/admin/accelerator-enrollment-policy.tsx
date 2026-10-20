import { useEffect, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useAuth } from "@/context/auth"
import { getAcceleratorPolicy } from "@/lib/accelerator-api"
import type { AcceleratorPolicy } from "@/types/accelerator"
import { AcceleratorPolicyEditor } from "./accelerator-policy-editor"
import { useAcceleratorText } from "@/pages/accelerator-text"
export function AcceleratorEnrollmentPolicy() {
 const { principal } = useAuth(), t = useAcceleratorText(), owner = principal?.user.role === "owner"
 const identity = `${principal?.user.id}/${principal?.session.id}`
 const [state, setState] = useState<{ identity: string; data?: AcceleratorPolicy; error?: string }>({ identity }), [revision, setRevision] = useState(0)
 useEffect(() => {
  const controller = new AbortController(); setState({ identity })
  if (owner) void getAcceleratorPolicy({ kind: "enrollment_policy" }, controller.signal).then(data => { if (!controller.signal.aborted) setState({ identity, data }) }).catch((failure: unknown) => { if (!controller.signal.aborted) setState({ identity, error: failure instanceof Error ? failure.message : "Policy unavailable" }) })
  return () => controller.abort()
 }, [identity, owner, revision])
 if (!owner) return null
 const reload = () => setRevision(value => value + 1), data = state.identity === identity ? state.data : undefined
 return <Card><CardHeader><CardTitle>{t("enrollmentTitle")}</CardTitle><p className="text-sm text-muted-foreground">{t("enrollmentNote")}</p></CardHeader><CardContent>
  {state.error ? <p role="alert">{state.error}<Button variant="outline" onClick={reload}>{t("retry")}</Button></p> : data ? <AcceleratorPolicyEditor key={`${identity}/${data.revision}`} value={data} scope={{ kind: "enrollment_policy" }} saved={reload} /> : <Skeleton className="h-64" />}
 </CardContent></Card>
}
