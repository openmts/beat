import { BrowserRouter, Route, Routes } from "react-router"
import { ThemeProvider } from "@/context/theme"
import { LocaleProvider } from "@/context/locale"
import { SiteSettingsProvider, useSiteSettings } from "@/context/site-settings"
import { TooltipProvider } from "@/components/ui/tooltip"
import { PublicPWA } from "@/components/public-pwa"
import { PublicTelemetryTracker } from "@/components/public-telemetry-tracker"
import { LazyRoute } from "@/components/route-chunk"
import { loadDashboard, loadNodeDetail, loadAdmin, loadNotFound } from "@/lib/route-loaders"
import "@/index.css"

function App() {
  return (
    <SiteSettingsProvider>
      <ConfiguredApp />
    </SiteSettingsProvider>
  )
}

function ConfiguredApp() {
  const { settings } = useSiteSettings()
  return (
    <ThemeProvider defaultTheme={settings.default_theme}>
      <LocaleProvider>
        <TooltipProvider delay={300}>
          <BrowserRouter>
            <PublicPWA />
            <PublicTelemetryTracker />
            <Routes>
              <Route path="/" element={<LazyRoute load={loadDashboard} />} />
              <Route path="/node/:id" element={<LazyRoute load={loadNodeDetail} />} />
              <Route path="/metrics/snapshots/:token" element={<LazyRoute load={loadNotFound} />} />
              <Route path="/admin/*" element={<LazyRoute load={loadAdmin} />} />
              <Route path="*" element={<LazyRoute load={loadNotFound} />} />
            </Routes>
          </BrowserRouter>
        </TooltipProvider>
      </LocaleProvider>
    </ThemeProvider>
  )
}

export default App
