import { useState } from "react"
import { useSSHKeys } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent } from "@/components/ui/card"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from "@/components/ui/dialog"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { PlusIcon, Trash2Icon, CopyIcon, EyeIcon, CheckIcon } from "lucide-react"
import { PageHeader } from "@/components/page-header"
import type { GeneratedSSHKey } from "@/types"

const keyTypeOptions = [
  { label: "Ed25519", value: "ed25519" },
  { label: "RSA 2048", value: "rsa" },
]

interface ViewKeyData {
  name: string
  publicKey: string
  fingerprint: string
}

function SSHKeys() {
  const { data: keys, loading, error, refresh } = useSSHKeys()
  const { t } = useLocale()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [viewKey, setViewKey] = useState<ViewKeyData | null>(null)
  const [generatedKey, setGeneratedKey] = useState<GeneratedSSHKey | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [tab, setTab] = useState("import")
  const [importName, setImportName] = useState("")
  const [importPublic, setImportPublic] = useState("")
  const [importPrivate, setImportPrivate] = useState("")
  const [genName, setGenName] = useState("")
  const [genType, setGenType] = useState("ed25519")
  const [copiedKey, setCopiedKey] = useState<"public" | "private" | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const handleImport = async () => {
    if (!importName.trim() || !importPublic.trim()) return
    setActionError(null)
    setActionLoading(true)
    try {
      await api.createSSHKey({
        name: importName.trim(),
        public_key: importPublic.trim(),
        private_key: importPrivate.trim() || undefined,
        key_type: "imported",
      })
      await refresh()
      setDialogOpen(false)
      resetForm()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleGenerate = async () => {
    if (!genName.trim()) return
    setActionError(null)
    setActionLoading(true)
    try {
      const result = await api.generateSSHKey(genName.trim(), genType)
      await refresh()
      setDialogOpen(false)
      setGeneratedKey(result)
      resetForm()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteId) return
    setActionError(null)
    setActionLoading(true)
    try {
      await api.deleteSSHKey(deleteId)
      await refresh()
      setDeleteId(null)
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
    } catch (err) {
      setActionError(messageFromError(err))
    }
  }

  const handleCopyKey = async (kind: "public" | "private", text: string) => {
    await handleCopy(text)
    setCopiedKey(kind)
    window.setTimeout(() => setCopiedKey(null), 1500)
  }

  const resetForm = () => {
    setImportName("")
    setImportPublic("")
    setImportPrivate("")
    setGenName("")
    setGenType("ed25519")
    setTab("import")
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("ssh.keys")} description={t("ssh.keys_description")}>
        <Button
          onClick={() => {
            resetForm()
            setDialogOpen(true)
          }}
        >
          <PlusIcon data-icon="inline-start" />
          {t("app.create")}
        </Button>
      </PageHeader>
      {actionError && (
        <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>
      )}

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("node.name")}</TableHead>
                <TableHead>{t("app.type")}</TableHead>
                <TableHead>{t("ssh.fingerprint")}</TableHead>
                <TableHead className="w-40">{t("app.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-48" /></TableCell>
                    <TableCell><Skeleton className="h-8 w-32" /></TableCell>
                  </TableRow>
                ))
              ) : keys && keys.length > 0 ? (
                keys.map((key) => (
                  <TableRow key={key.id}>
                    <TableCell className="font-medium">{key.name}</TableCell>
                    <TableCell>{key.key_type}</TableCell>
                    <TableCell className="max-w-[12rem] truncate font-mono text-xs text-muted-foreground sm:max-w-none">
                      {key.fingerprint || "-"}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => handleCopy(key.public_key)}
                          aria-label={t("ssh.copy_key")}
                          title={t("ssh.copy_key")}
                        >
                          <CopyIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setViewKey({ name: key.name, publicKey: key.public_key, fingerprint: key.fingerprint })}
                          aria-label={t("ssh.fingerprint")}
                          title={t("ssh.fingerprint")}
                        >
                          <EyeIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setDeleteId(key.id)}
                          title={t("app.delete")}
                        >
                          <Trash2Icon className="text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-muted-foreground">
                    {t("app.no_data")}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("ssh.keys")}</DialogTitle>
          </DialogHeader>
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList className="w-full">
              <TabsTrigger value="import" className="flex-1">{t("ssh.import")}</TabsTrigger>
              <TabsTrigger value="generate" className="flex-1">{t("ssh.generate")}</TabsTrigger>
            </TabsList>
            <TabsContent value="import" className="mt-4 flex flex-col gap-4">
              <div className="flex flex-col gap-2">
                <Label htmlFor="import-name">{t("node.name")}</Label>
                <Input id="import-name" value={importName} onChange={(e) => setImportName(e.target.value)} />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="import-public">{t("ssh.public_key")}</Label>
                <Input id="import-public" value={importPublic} onChange={(e) => setImportPublic(e.target.value)} placeholder="ssh-rsa AAA..." />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="import-private">{t("ssh.private_key")}</Label>
                <Input id="import-private" value={importPrivate} onChange={(e) => setImportPrivate(e.target.value)} placeholder="-----BEGIN..." />
              </div>
            </TabsContent>
            <TabsContent value="generate" className="mt-4 flex flex-col gap-4">
              <div className="flex flex-col gap-2">
                <Label htmlFor="gen-name">{t("node.name")}</Label>
                <Input id="gen-name" value={genName} onChange={(e) => setGenName(e.target.value)} />
              </div>
              <div className="flex flex-col gap-2">
                <Label>{t("ssh.key_type")}</Label>
                <Select
                  items={keyTypeOptions}
                  value={genType}
                  onValueChange={(v) => { if (v) setGenType(v) }}
                >
                  <SelectTrigger aria-label={t("ssh.key_type")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {keyTypeOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
            </TabsContent>
          </Tabs>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>{t("app.cancel")}</Button>
            <Button
              onClick={tab === "import" ? handleImport : handleGenerate}
              disabled={actionLoading || (tab === "import" ? !importName.trim() || !importPublic.trim() : !genName.trim())}
            >
              {t("app.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={viewKey !== null} onOpenChange={(v) => { if (!v) setViewKey(null) }}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{viewKey?.name}</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-4">
            <KeyField label={t("ssh.fingerprint")} value={viewKey?.fingerprint || "-"} />
            <KeyField label={t("ssh.public_key")} value={viewKey?.publicKey || ""} copyable />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setViewKey(null)}>{t("app.cancel")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={generatedKey !== null} onOpenChange={(v) => { if (!v) setGeneratedKey(null) }}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t("ssh.generated_title")}</DialogTitle>
            <DialogDescription>{t("ssh.generated_description")}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-4">
            <KeyField
              label={t("ssh.public_key")}
              value={generatedKey?.public_key || ""}
              copyable
              copied={copiedKey === "public"}
              onCopy={() => handleCopyKey("public", generatedKey?.public_key || "")}
            />
            <KeyField
              label={t("ssh.private_key")}
              value={generatedKey?.private_key || ""}
              copyable
              copied={copiedKey === "private"}
              onCopy={() => handleCopyKey("private", generatedKey?.private_key || "")}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setGeneratedKey(null)}>{t("app.cancel")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!deleteId} onOpenChange={(v) => { if (!v) setDeleteId(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("confirm.delete")}</DialogTitle>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteId(null)}>{t("app.cancel")}</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={actionLoading}>
              {t("app.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function KeyField({ label, value, copyable = false, copied = false, onCopy }: {
  label: string
  value: string
  copyable?: boolean
  copied?: boolean
  onCopy?: () => void
}) {
  const { t } = useLocale()
  return (
    <div className="min-w-0">
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <Label>{label}</Label>
        {copyable && onCopy ? (
          <Button variant="ghost" size="sm" onClick={onCopy} aria-label={label}>
            {copied ? <CheckIcon data-icon="inline-start" /> : <CopyIcon data-icon="inline-start" />}
            {copied ? t("ssh.copied") : t("ssh.copy_key")}
          </Button>
        ) : null}
      </div>
      <pre className="max-h-48 min-w-0 overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted p-3 text-xs leading-relaxed">
        {value || "--"}
      </pre>
    </div>
  )
}

export default SSHKeys
function messageFromError(err: unknown) {
  return err instanceof Error ? err.message : "The request failed"
}
