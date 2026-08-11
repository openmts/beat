import { BrowserRouter, Route, Routes } from "react-router"
import { ThemeProvider } from "@/context/theme"
import { LocaleProvider } from "@/context/locale"
import { SiteSettingsProvider, useSiteSettings } from "@/context/site-settings"
import { TooltipProvider } from "@/components/ui/tooltip"
import Dashboard from "@/pages/dashboard"
import NodeDetail from "@/pages/node-detail"
import Admin from "@/pages/admin"
import NotFound from "@/pages/not-found"
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
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/node/:id" element={<NodeDetail />} />
              <Route path="/admin/*" element={<Admin />} />
              <Route path="*" element={<NotFound />} />
            </Routes>
          </BrowserRouter>
        </TooltipProvider>
      </LocaleProvider>
    </ThemeProvider>
  )
}

export default App
