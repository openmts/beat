import { useRef, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"

export function ThemeUploadDialog({ open, onOpenChange, onUpload }: { open: boolean; onOpenChange: (open: boolean) => void; onUpload: (file: File) => void }) {
  const { t } = useLocale()
  const file = useRef<HTMLInputElement>(null)
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const archive = file.current?.files?.[0]
    if (archive) { onOpenChange(false); onUpload(archive) }
  }
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent><DialogHeader>
    <DialogTitle>{t("theme.upload_title")}</DialogTitle><DialogDescription>{t("theme.upload_description")}</DialogDescription>
  </DialogHeader><form onSubmit={submit}><FieldGroup><Field><FieldLabel htmlFor="theme-archive">{t("theme.upload")}</FieldLabel>
    <Input id="theme-archive" type="file" accept=".zip" required ref={file} /></Field><Button type="submit">{t("theme.upload")}</Button>
  </FieldGroup></form></DialogContent></Dialog>
}
