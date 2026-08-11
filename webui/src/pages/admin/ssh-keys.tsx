import { useState } from "react"
import { useSSHKeys } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent } from "@/components/ui/card"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { PlusIcon, Trash2Icon, CopyIcon, EyeIcon } from "lucide-react"
import { PageHeader } from "@/components/page-header"

const keyTypeOptions = [
  { label: "Ed25519", value: "ed25519" },
  { label: "RSA 2048", value: "rsa" },
]
function SSHKeys() {
  const { data: keys, loading, error, refresh } = useSSHKeys()
  const { t } = useLocale()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [viewKey, setViewKey] = useState<{ name: string; publicKey: string; fingerprint: string } | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [tab, setTab] = useState("import")
  const [importName, setImportName] = useState("")
  const [importPublic, setImportPublic] = useState("")
  const [importPrivate, setImportPrivate] = useState("")
  const [genName, setGenName] = useState("")
  const [genType, setGenType] = useState("ed25519")
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
      await api.generateSSHKey(genName.trim(), genType)
      await refresh()
      setDialogOpen(false)
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
                    <TableCell className="font-mono text-xs text-muted-foreground">
                      {key.fingerprint || "-"}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => handleCopy(key.public_key)}
                        >
                          <CopyIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setViewKey({ name: key.name, publicKey: key.public_key, fingerprint: key.fingerprint })}
                        >
                          <EyeIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setDeleteId(key.id)}
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
            <TabsContent value="import" className="flex flex-col gap-4 mt-4">
              <div className="flex flex-col gap-2">
                <Label>{t("node.name")}</Label>
                <Input value={importName} onChange={(e) => setImportName(e.target.value)} />
              </div>
              <div className="flex flex-col gap-2">
                <Label>{t("ssh.public_key")}</Label>
                <Input value={importPublic} onChange={(e) => setImportPublic(e.target.value)} placeholder="ssh-rsa AAA..." />
              </div>
              <div className="flex flex-col gap-2">
                <Label>{t("ssh.private_key")}</Label>
                <Input value={importPrivate} onChange={(e) => setImportPrivate(e.target.value)} placeholder="-----BEGIN..." />
              </div>
            </TabsContent>
            <TabsContent value="generate" className="flex flex-col gap-4 mt-4">
              <div className="flex flex-col gap-2">
                <Label>{t("node.name")}</Label>
                <Input value={genName} onChange={(e) => setGenName(e.target.value)} />
              </div>
              <div className="flex flex-col gap-2">
                <Label>{t("ssh.key_type")}</Label>
                <Select
                  items={keyTypeOptions}
                  value={genType}
                  onValueChange={(v) => { if (v) setGenType(v) }}
                >
                  <SelectTrigger>
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

      <Dialog open={!!viewKey} onOpenChange={(v) => { if (!v) setViewKey(null) }}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{viewKey?.name}</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label>{t("ssh.fingerprint")}</Label>
            <p className="font-mono text-xs text-muted-foreground">{viewKey?.fingerprint || "-"}</p>
          </div>
          <div className="flex flex-col gap-2">
            <Label>{t("ssh.public_key")}</Label>
            <pre className="max-h-40 overflow-auto rounded bg-muted p-3 font-mono text-xs">{viewKey?.publicKey}</pre>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setViewKey(null)}>{t("app.cancel")}</Button>
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
export default SSHKeys
function messageFromError(err: unknown) {
  return err instanceof Error ? err.message : "The request failed"
}
