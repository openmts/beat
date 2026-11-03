import { useCallback, useEffect, useState, type FormEvent } from "react";
import { ActivityIcon, SaveIcon, ShieldCheckIcon } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useOptionalAuth } from "@/context/auth";
import { useLocale } from "@/context/locale";
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation";
import { APIError } from "@/lib/api-error";
import {
  getEgressProxy,
  testEgressProxy,
  updateEgressProxy,
} from "@/lib/egress-proxy-api";
import type { EgressProxySettings } from "@/types/egress-proxy";
import {
  egressProxyFormFromSettings,
  emptyEgressProxyForm,
  type EgressProxyFormState,
} from "./egress-proxy-form";
import { EgressProxyFields } from "./egress-proxy-fields";
import { OperationConfirm } from "./operation-confirm";

// EgressProxyPanel 只向 owner 提供后台出站代理配置。密码只提交一次，
// 服务端仅在 has_password 中披露是否已保存，任何接口都不会回传明文。
export function EgressProxyPanel() {
  const auth = useOptionalAuth();
  if (auth?.principal?.user.role !== "owner") return null;
  return <OwnerEgressProxyPanel />;
}

function OwnerEgressProxyPanel() {
  const { principal } = useOptionalAuth()!;
  const { t } = useLocale();
  const flow = useConfirmedOperation();
  const [settings, setSettings] = useState<EgressProxySettings | null>(null);
  const [form, setForm] =
    useState<EgressProxyFormState>(emptyEgressProxyForm);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [loadFailed, setLoadFailed] = useState(false);
  const [loadError, setLoadError] = useState("");
  const owner = principal?.user.role === "owner";

  const load = useCallback(
    async (signal?: AbortSignal) => {
      if (!owner) return;
      setLoading(true);
      setError("");
      setLoadFailed(false);
      setLoadError("");
      try {
        const loaded = await getEgressProxy(signal);
        setSettings(loaded);
        setForm(egressProxyFormFromSettings(loaded));
      } catch (reason) {
        if (signal?.aborted) return;
        setLoadFailed(true);
        setLoadError(reason instanceof Error ? reason.message : "");
      } finally {
        if (!signal?.aborted) setLoading(false);
      }
    },
    [owner],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  if (!owner) return null;

  const displayedError = loadFailed
    ? loadError || t("settings.proxy_unavailable")
    : error;

  const patch = (values: Partial<EgressProxyFormState>) =>
    setForm((current) => ({ ...current, ...values }));
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!settings || flow.busy) return;
    const snapshot = { ...form };
    flow.request({
      title: t("settings.proxy_confirm_title"),
      description: t("settings.proxy_confirm_description"),
      execute: async () => {
        setMessage("");
        setError("");
        try {
          const updated = await updateEgressProxy({
            enabled: snapshot.enabled,
            proxy_url: snapshot.proxy_url,
            username: snapshot.username,
            ...(snapshot.password ? { password: snapshot.password } : {}),
            clear_password: snapshot.clear_password,
            expected_revision: settings.revision,
          });
          setSettings(updated);
          setForm(egressProxyFormFromSettings(updated));
          setMessage(t("settings.proxy_saved"));
        } catch (reason) {
          if (reason instanceof APIError && reason.status === 409) {
            await load();
            setError(t("settings.proxy_conflict"));
            return;
          }
          setError(
            reason instanceof Error
              ? reason.message
              : t("settings.proxy_save_failed"),
          );
        }
      },
    });
  };
  const probe = () => {
    if (!settings?.enabled || !settings.proxy_url || flow.busy) return;
    flow.request({
      title: t("settings.proxy_test_title"),
      description: t("settings.proxy_test_description"),
      execute: async () => {
        setMessage("");
        setError("");
        try {
          const result = await testEgressProxy();
          setMessage(
            t("settings.proxy_test_success").replace(
              "{latency}",
              String(result.latency_ms),
            ),
          );
        } catch (reason) {
          if (reason instanceof APIError && reason.status === 428) throw reason;
          setError(
            reason instanceof Error
              ? reason.message
              : t("settings.proxy_test_failed"),
          );
        }
      },
    });
  };

  return (
    <>
      <Card className="mt-8 overflow-hidden border-border/80 shadow-none">
        <CardHeader className="gap-2 border-b bg-muted/20">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0 space-y-1.5">
              <CardTitle className="flex items-center gap-2 text-base">
                <ShieldCheckIcon className="size-4" aria-hidden="true" />
                {t("settings.proxy_title")}
              </CardTitle>
              <CardDescription className="max-w-3xl leading-relaxed">
                {t("settings.proxy_description")}
              </CardDescription>
            </div>
            <Badge variant={settings?.enabled ? "default" : "outline"}>
              {t(
                settings?.enabled
                  ? "settings.proxy_enabled_badge"
                  : "settings.proxy_disabled_badge",
              )}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="pt-5">
          {(displayedError || message) && (
            <Alert
              variant={displayedError ? "destructive" : "default"}
              className="mb-5"
            >
              <AlertDescription>{displayedError || message}</AlertDescription>
            </Alert>
          )}
          <form onSubmit={submit} aria-busy={loading || flow.busy}>
            <EgressProxyFields
              form={form}
              settings={settings}
              disabled={loading || flow.busy}
              onChange={patch}
            />
            <div className="mt-5 flex flex-col-reverse gap-3 border-t pt-5 sm:flex-row sm:items-center sm:justify-between">
              <p className="text-xs text-muted-foreground">
                {t("settings.proxy_revision").replace(
                  "{revision}",
                  String(settings?.revision ?? 0),
                )}
              </p>
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                <Button
                  type="button"
                  variant="outline"
                  onClick={probe}
                  disabled={
                    loading ||
                    flow.busy ||
                    !settings?.enabled ||
                    !settings.proxy_url
                  }
                  title={t("settings.proxy_test_hint")}
                >
                  <ActivityIcon />
                  {t("settings.proxy_test")}
                </Button>
                <Button
                  type="submit"
                  disabled={loading || flow.busy || !settings}
                >
                  <SaveIcon />
                  {t("settings.proxy_save")}
                </Button>
              </div>
            </div>
          </form>
        </CardContent>
      </Card>
      <OperationConfirm flow={flow} />
    </>
  );
}
