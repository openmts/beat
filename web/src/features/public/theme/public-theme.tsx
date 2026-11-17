import { createContext, useContext, useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import type { PublicThemeAppearance, PublicThemeDocument, PublicThemeMode } from "@/api/types";
import { cn } from "@/lib/utils";

export const DEFAULT_PUBLIC_THEME: PublicThemeDocument = {
  schemaVersion: 1,
  revision: 1,
  presetId: "minimal",
  appearance: {
    mode: "system",
    primary: "#18181b",
    accent: "#71717a",
    contentWidth: "wide",
    radius: "sm",
    surface: "solid",
    density: "compact",
  },
  home: {
    blocks: [
      { id: "groups", type: "group-filter", enabled: true, layout: { minHeight: "auto", padding: "none", columns: 0, gap: "sm" }, props: { variant: "minimal", title: "全部", description: "选择分组查看节点" } },
      { id: "summary", type: "summary-stats", enabled: true, layout: { minHeight: "auto", padding: "none", columns: 4, gap: "sm" }, props: { variant: "minimal", items: ["all", "online", "warning", "offline"] } },
      { id: "nodes", type: "node-collection", enabled: true, layout: { minHeight: "auto", padding: "none", columns: 4, gap: "sm" }, props: {
        variant: "minimal", view: "card", title: "节点", description: "点击节点查看指标详情", fields: ["status", "cpu", "memory", "disk", "network"],
      } },
    ],
  },
  nodeDetail: {
    blocks: [
      { id: "header", type: "node-header", enabled: true, layout: { minHeight: "auto", padding: "none", columns: 0, gap: "md" }, props: {} },
      { id: "overview", type: "system-info", enabled: true, layout: { minHeight: "auto", padding: "none", columns: 0, gap: "md" }, props: {} },
      { id: "summary", type: "metric-summary", enabled: true, layout: { minHeight: "auto", padding: "none", columns: 4, gap: "sm" }, props: {} },
      { id: "trends", type: "metric-trends", enabled: true, layout: { minHeight: "auto", padding: "none", columns: 2, gap: "md" }, props: { defaultRange: "24h", charts: ["cpu", "memory", "disk", "network", "load"] } },
      { id: "probes", type: "network-probes", enabled: true, layout: { minHeight: "auto", padding: "none", columns: 0, gap: "md" }, props: {} },
    ],
  },
};

type ThemeContextValue = {
  document: PublicThemeDocument;
  resolvedMode: "light" | "dark";
  toggleMode: () => void;
};

const ThemeContext = createContext<ThemeContextValue>({
  document: DEFAULT_PUBLIC_THEME,
  resolvedMode: "light",
  toggleMode: () => undefined,
});

export function PublicThemeRoot({ document, children, className }: { document: PublicThemeDocument; children: ReactNode; className?: string }) {
  const [mode, setMode] = useState<PublicThemeMode>(document.appearance.mode);
  const [systemDark, setSystemDark] = useState(() => window.matchMedia("(prefers-color-scheme: dark)").matches);

  useEffect(() => {
    setMode(document.appearance.mode);
  }, [document.appearance.mode]);

  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const update = () => setSystemDark(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  const resolvedMode = mode === "system" ? (systemDark ? "dark" : "light") : mode;
  const value = useMemo(() => ({
    document,
    resolvedMode,
    toggleMode: () => setMode(resolvedMode === "dark" ? "light" : "dark"),
  }), [document, resolvedMode]);

  return (
    <ThemeContext.Provider value={value}>
      <div
        data-public-theme=""
        data-preset={document.presetId ?? "custom"}
        data-surface={document.appearance.surface}
        data-density={document.appearance.density}
        style={themeStyle(document.appearance, resolvedMode, document.presetId)}
        className={cn(
          "public-theme min-h-screen bg-background text-foreground",
          resolvedMode === "dark" && "dark",
          publicSurfaceClass(document.appearance.surface),
          className,
        )}
      >
        {children}
      </div>
    </ThemeContext.Provider>
  );
}

export function usePublicTheme() {
  return useContext(ThemeContext);
}

export function publicContentWidthClass(width: PublicThemeAppearance["contentWidth"]) {
  if (width === "full") return "max-w-[1680px]";
  if (width === "standard") return "max-w-[1120px]";
  return "max-w-[1360px]";
}

function publicSurfaceClass(surface: PublicThemeAppearance["surface"]) {
  if (surface === "solid") return "public-surface-solid";
  if (surface === "mesh") return "public-surface-mesh";
  return "public-surface-gradient";
}

function themeStyle(appearance: PublicThemeAppearance, mode: "light" | "dark", presetId?: string): CSSProperties {
  const minimal = presetId === "minimal";
  const primary = minimal && mode === "dark" ? "0 0% 98%" : hexToHsl(appearance.primary);
  const accent = minimal && mode === "dark" ? "240 5% 65%" : hexToHsl(appearance.accent);
  const palette = minimal
    ? (mode === "dark" ? minimalDarkPalette : minimalLightPalette)
    : (mode === "dark" ? darkPalette : lightPalette);
  const radius = appearance.radius === "sm" ? "0.375rem" : appearance.radius === "lg" ? "0.9rem" : "0.625rem";
  return {
    "--background": palette.background,
    "--foreground": palette.foreground,
    "--card": palette.card,
    "--card-foreground": palette.foreground,
    "--popover": palette.card,
    "--popover-foreground": palette.foreground,
    "--primary": primary,
    "--primary-foreground": mode === "dark" ? (minimal ? "240 6% 10%" : "222 36% 10%") : "0 0% 100%",
    "--secondary": palette.secondary,
    "--secondary-foreground": palette.foreground,
    "--muted": palette.muted,
    "--muted-foreground": palette.mutedForeground,
    "--accent": palette.accent,
    "--accent-foreground": palette.foreground,
    "--border": palette.border,
    "--input": palette.border,
    "--ring": primary,
    "--radius": radius,
    "--chart-cpu": palette.chartCpu,
    "--chart-memory": palette.chartMemory,
    "--chart-disk": palette.chartDisk,
    "--chart-network": palette.chartNetwork,
    "--public-accent": accent,
  } as CSSProperties;
}

const lightPalette = {
  background: "216 33% 97%",
  foreground: "220 27% 14%",
  card: "0 0% 100%",
  secondary: "215 25% 95%",
  muted: "215 24% 95%",
  mutedForeground: "218 11% 42%",
  accent: "214 100% 96%",
  border: "216 20% 89%",
  success: "152 65% 30%",
  warning: "35 92% 34%",
  chartCpu: "214 90% 50%",
  chartMemory: "166 72% 36%",
  chartDisk: "35 92% 46%",
  chartNetwork: "262 72% 58%",
};

const darkPalette = {
  background: "222 28% 9%",
  foreground: "214 26% 94%",
  card: "222 26% 12%",
  secondary: "218 22% 17%",
  muted: "218 21% 16%",
  mutedForeground: "215 17% 68%",
  accent: "216 31% 20%",
  border: "217 20% 24%",
  success: "152 56% 66%",
  warning: "38 92% 62%",
  chartCpu: "214 92% 70%",
  chartMemory: "166 62% 58%",
  chartDisk: "38 92% 62%",
  chartNetwork: "262 78% 72%",
};

const minimalLightPalette = {
  background: "0 0% 100%",
  foreground: "240 6% 10%",
  card: "0 0% 100%",
  secondary: "240 5% 96%",
  muted: "240 5% 96%",
  mutedForeground: "240 4% 46%",
  accent: "240 5% 96%",
  border: "240 6% 90%",
  success: "152 55% 34%",
  warning: "35 83% 38%",
  chartCpu: "240 6% 10%",
  chartMemory: "240 4% 46%",
  chartDisk: "240 5% 65%",
  chartNetwork: "240 6% 24%",
};

const minimalDarkPalette = {
  background: "240 10% 4%",
  foreground: "0 0% 98%",
  card: "240 7% 7%",
  secondary: "240 5% 12%",
  muted: "240 5% 12%",
  mutedForeground: "240 5% 65%",
  accent: "240 5% 12%",
  border: "240 4% 16%",
  success: "152 55% 60%",
  warning: "38 92% 62%",
  chartCpu: "0 0% 98%",
  chartMemory: "240 5% 65%",
  chartDisk: "240 4% 46%",
  chartNetwork: "240 5% 80%",
};

function hexToHsl(hex: string): string {
  const match = /^#?([0-9a-f]{6})$/i.exec(hex);
  if (!match) return "214 90% 43%";
  const value = Number.parseInt(match[1], 16);
  const red = ((value >> 16) & 255) / 255;
  const green = ((value >> 8) & 255) / 255;
  const blue = (value & 255) / 255;
  const max = Math.max(red, green, blue);
  const min = Math.min(red, green, blue);
  const delta = max - min;
  let hue = 0;
  if (delta !== 0) {
    if (max === red) hue = ((green - blue) / delta) % 6;
    else if (max === green) hue = (blue - red) / delta + 2;
    else hue = (red - green) / delta + 4;
  }
  hue = Math.round(hue * 60);
  if (hue < 0) hue += 360;
  const lightness = (max + min) / 2;
  const saturation = delta === 0 ? 0 : delta / (1 - Math.abs(2 * lightness - 1));
  return `${hue} ${Math.round(saturation * 100)}% ${Math.round(lightness * 100)}%`;
}
