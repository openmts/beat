export const siteSettingsTranslations: Record<
  string,
  Record<string, string>
> = {
  en: {
    "settings.site": "Site settings",
    "settings.site_description":
      "Public identity, branding, and visitor presentation.",
    "settings.identity": "Identity",
    "settings.site_title": "Site title",
    "settings.site_title_hint":
      "Shown in the browser title and application header.",
    "settings.description": "Site description",
    "settings.description_hint":
      "Used as the page description for browsers and search previews.",
    "settings.branding": "Branding",
    "settings.logo_url": "Logo URL",
    "settings.logo_hint": "Use an HTTPS URL or a root-relative path.",
    "settings.favicon_url": "Favicon URL",
    "settings.favicon_hint": "Shown in browser tabs and bookmarks.",
    "settings.appearance": "Appearance",
    "settings.default_theme": "Default theme",
    "settings.theme_system": "Follow system",
    "settings.theme_light": "Light",
    "settings.theme_dark": "Dark",
    "settings.default_theme_hint":
      "Applies until a visitor chooses a local theme.",
    "settings.public_display": "Public display",
    "settings.show_ip": "Show node IP addresses",
    "settings.show_ip_hint":
      "When disabled, public REST and live responses redact node hosts.",
    "settings.address_mode": "Public node address",
    "settings.address_hidden": "Hidden",
    "settings.address_masked": "Masked",
    "settings.address_full": "Full",
    "settings.address_hint":
      "Hidden removes the address, masked keeps only the network prefix (first IPv4 octet or the first 32 IPv6 bits), full publishes the stored value.",
    "settings.deployment": "Deployment access",
    "settings.deployment_hint":
      "These values come from the server environment. They cannot be edited here and the origin list is never sent to the browser.",
    "settings.deployment_unavailable": "Unavailable",
    "settings.external_url": "External URL",
    "settings.external_url_unset": "Not configured",
    "settings.api_origins": "Public REST origins",
    "settings.ws_origins": "Public WebSocket origins",
    "settings.origins_all": "All public origins",
    "settings.origins_same": "Same origin",
    "settings.origins_configured": "{count} configured origins",
    "settings.address_privacy_note":
      "Visitor identity and activity are never logged. Public telemetry stays aggregate-only and no analytics sample is written to SQLite.",
    "settings.show_network": "Show network quality",
    "settings.show_network_hint":
      "Controls the network quality section on the public dashboard.",
    "settings.show_accelerators": "Show GPU and accelerators",
    "settings.show_accelerators_hint":
      "Global override. When off, metric pages hide GPU and accelerator sections for every node.",
    "settings.accelerator_node_visibility": "Show this node in metric pages",
    "settings.accelerator_node_visibility_hint":
      "When the global switch is on, controls whether this node's GPU and accelerator section is visible.",
    "settings.accelerator_node_global_off":
      "The global switch is off. This node setting is saved and takes effect when the global switch is turned back on.",
    "settings.public_telemetry": "Anonymous aggregate telemetry",
    "settings.public_telemetry_hint":
      "Counts a fixed set of coarse page events. No identity, address, URL, or free-form detail is ever collected.",
    "settings.telemetry_enabled": "Accept anonymous interaction counts",
    "settings.telemetry_ceiling": "Session sample ceiling",
    "settings.telemetry_ceiling_hint":
      "Applies per browser session; the server also rate limits each source.",
    "settings.telemetry_retention": "Aggregate retention (days)",
    "settings.telemetry_retention_hint":
      "Aggregate counters are pruned after this many days.",
    "settings.telemetry_proof_hint":
      "Confirm your identity with the current password and authenticator code before changing the telemetry policy.",
    "settings.telemetry_password": "Current password",
    "settings.telemetry_code": "Authenticator code",
    "settings.telemetry_cancel": "Cancel",
    "settings.telemetry_confirm_action": "Confirm",
    "settings.telemetry_save": "Save public telemetry policy",
    "settings.telemetry_saved": "Public telemetry policy saved",
    "settings.telemetry_unavailable": "Public telemetry policy unavailable",
    "settings.proxy_title": "Outbound proxy",
    "settings.proxy_description":
      "Route server-side downloads and integrations through one HTTP, HTTPS, SOCKS5, or SOCKS5H proxy. Changes apply to new requests immediately.",
    "settings.proxy_enabled_badge": "Enabled",
    "settings.proxy_disabled_badge": "Disabled",
    "settings.proxy_enable": "Use outbound proxy",
    "settings.proxy_enable_hint":
      "Uses the saved settings for new background requests without restarting the server.",
    "settings.proxy_url": "Proxy URL",
    "settings.proxy_url_hint":
      "Include the scheme and port, for example http://127.0.0.1:7890 or socks5h://proxy.example:1080.",
    "settings.proxy_username": "Username",
    "settings.proxy_username_hint":
      "Optional. Credentials are stored with the server application secret key.",
    "settings.proxy_password": "Password",
    "settings.proxy_password_saved":
      "A password is saved. Leave this blank to keep it unchanged.",
    "settings.proxy_password_hint":
      "Optional. Leave blank when the proxy does not require authentication.",
    "settings.proxy_clear_password": "Clear the saved password",
    "settings.proxy_clear_password_hint":
      "The username remains saved. Use this when authentication is no longer required.",
    "settings.proxy_scope":
      "Applies to MMDB and MaxMind downloads, GeoIP lookups, agent releases, theme markets, webhooks, and OIDC. SMTP and SSH connections keep their native transport.",
    "settings.proxy_revision": "Configuration revision {revision}",
    "settings.proxy_save": "Save proxy settings",
    "settings.proxy_saved": "Outbound proxy settings saved",
    "settings.proxy_conflict":
      "The settings changed in another session. The latest values were reloaded; review and save again.",
    "settings.proxy_save_failed": "Could not save outbound proxy settings",
    "settings.proxy_unavailable": "Outbound proxy settings are unavailable",
    "settings.proxy_confirm_title": "Save outbound proxy",
    "settings.proxy_confirm_description":
      "Confirm this owner-only network routing change.",
    "settings.proxy_test": "Test saved proxy",
    "settings.proxy_test_hint":
      "Tests the saved configuration. Save unsaved changes first.",
    "settings.proxy_test_title": "Test outbound proxy",
    "settings.proxy_test_description":
      "Beat will request a fixed public example.com page through the saved proxy. Proxy credentials are never returned.",
    "settings.proxy_test_success": "Proxy connection succeeded in {latency} ms",
    "settings.proxy_test_failed": "Could not test the outbound proxy",
    "settings.saved": "Site settings saved",
    "settings.preview": "Brand preview",
  },
};
