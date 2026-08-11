import { useCallback, useEffect, useState, type FormEvent } from "react"
import { PlusIcon, Trash2Icon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useLocale } from "@/context/locale"
import { createAdminUser, deleteAdminUser, listAdminUsers } from "@/lib/security-api"
import type { AdminRole, AdminUser } from "@/types/security"

export function SecurityAdminsPanel() {
  const { t } = useLocale()
  const [users, setUsers] = useState<AdminUser[]>([])
  const [username, setUsername] = useState("")
  const [displayName, setDisplayName] = useState("")
  const [password, setPassword] = useState("")
  const [role, setRole] = useState<AdminRole>("admin")
  const [error, setError] = useState("")

  const load = useCallback(async () => {
    try {
      setUsers(await listAdminUsers())
      setError("")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }, [t])

  useEffect(() => { void load() }, [load])

  const remove = async (id: string) => {
    try {
      await deleteAdminUser(id)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const create = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await createAdminUser({ username, display_name: displayName, role, password })
      setUsername("")
      setDisplayName("")
      setPassword("")
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const roleOptions = [
    { label: t("security.admin"), value: "admin" },
    { label: t("security.owner"), value: "owner" },
  ]

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(18rem,24rem)_1fr]">
      <Card>
        <CardHeader>
          <CardTitle>{t("security.create_admin")}</CardTitle>
          <CardDescription>{t("security.description")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={create}>
            <FieldGroup>
              {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
              <Field><FieldLabel htmlFor="new-admin-username">{t("security.username")}</FieldLabel>
                <Input id="new-admin-username" value={username} onChange={(event) => setUsername(event.target.value)} required /></Field>
              <Field><FieldLabel htmlFor="new-admin-name">{t("security.display_name")}</FieldLabel>
                <Input id="new-admin-name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} required /></Field>
              <Field><FieldLabel htmlFor="new-admin-password">{t("security.password")}</FieldLabel>
                <Input id="new-admin-password" type="password" minLength={12} value={password}
                  onChange={(event) => setPassword(event.target.value)} required /></Field>
              <Field><FieldLabel>{t("security.role")}</FieldLabel>
                <Select items={roleOptions} value={role} onValueChange={(value) => value && setRole(value as AdminRole)}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>{roleOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                  ))}</SelectGroup></SelectContent>
                </Select></Field>
              <Button type="submit"><PlusIcon data-icon="inline-start" />{t("security.create_admin")}</Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
      <div className="grid content-start gap-3 md:grid-cols-2">
        {users.map((user) => (
          <Card key={user.id}>
            <CardHeader>
              <div className="flex items-center justify-between gap-2">
                <CardTitle className="truncate text-sm">{user.display_name}</CardTitle>
                <Badge variant={user.enabled ? "default" : "secondary"}>
                  {user.enabled ? t("security.enabled") : t("security.disabled")}
                </Badge>
              </div>
              <CardDescription>@{user.username} · {t(`security.${user.role}`)}</CardDescription>
            </CardHeader>
            <CardContent className="flex justify-end">
              <Button variant="ghost" size="icon-sm" aria-label={t("app.delete")}
                onClick={() => void remove(user.id)}>
                <Trash2Icon />
              </Button>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
