import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider, useLocale } from "@/context/locale";
import { APIError } from "@/lib/api-error";
import {
  getEgressProxy,
  testEgressProxy,
  updateEgressProxy,
} from "@/lib/egress-proxy-api";
import type { EgressProxySettings } from "@/types/egress-proxy";
import { EgressProxyPanel } from "./egress-proxy-panel";

const state = vi.hoisted(() => ({ role: "owner" as "owner" | "admin" }));
vi.mock("@/context/auth", () => ({
  useOptionalAuth: () => ({
    principal: {
      user: { id: "owner", role: state.role, totp_enabled_at: null },
      session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" },
    },
  }),
  useAuth: () => ({
    principal: {
      user: { id: "owner", role: state.role, totp_enabled_at: null },
      session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" },
    },
  }),
}));
vi.mock("@/lib/egress-proxy-api", () => ({
  getEgressProxy: vi.fn(),
  testEgressProxy: vi.fn(),
  updateEgressProxy: vi.fn(),
}));
vi.mock("@/components/ui/switch", () => ({
  Switch: ({
    checked,
    onCheckedChange,
    ...props
  }: {
    checked: boolean;
    onCheckedChange?: (checked: boolean) => void;
  }) => (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onCheckedChange?.(!checked)}
      {...props}
    >
      proxy switch
    </button>
  ),
}));

const configured: EgressProxySettings = {
  enabled: true,
  proxy_url: "http://127.0.0.1:7890",
  username: "proxy-user",
  has_password: true,
  revision: 4,
  updated_at: "2026-10-23T15:00:00Z",
};

function view({ withLocaleToggle = false } = {}) {
  return render(
    <LocaleProvider>
      <EgressProxyPanel />
      {withLocaleToggle && <LocaleToggle />}
    </LocaleProvider>,
  );
}

function LocaleToggle() {
  const { locale, setLocale } = useLocale();
  return (
    <button
      type="button"
      onClick={() => setLocale(locale === "en" ? "zh-CN" : "en")}
    >
      Switch locale
    </button>
  );
}

async function saveAndConfirm(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Save proxy settings" }));
  await user.click(screen.getByRole("button", { name: "Confirm" }));
}

describe("egress proxy settings", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    state.role = "owner";
    vi.mocked(getEgressProxy).mockResolvedValue(configured);
    vi.mocked(testEgressProxy).mockResolvedValue({
      checked_at: "2026-10-24T12:00:00Z",
      latency_ms: 42,
    });
    vi.mocked(updateEgressProxy).mockImplementation(async (input) => ({
      ...configured,
      ...input,
      has_password:
        Boolean(input.password) ||
        (configured.has_password && !input.clear_password),
      revision: input.expected_revision + 1,
    }));
  });

  it("loads owner settings and omits an empty password while preserving the saved secret", async () => {
    const user = userEvent.setup();
    view();
    expect(
      await screen.findByDisplayValue("http://127.0.0.1:7890"),
    ).toBeInTheDocument();
    expect(screen.getByText(/A password is saved/)).toBeInTheDocument();
    await saveAndConfirm(user);
    await waitFor(() =>
      expect(updateEgressProxy).toHaveBeenCalledWith({
        enabled: true,
        proxy_url: "http://127.0.0.1:7890",
        username: "proxy-user",
        clear_password: false,
        expected_revision: 4,
      }),
    );
    expect(
      await screen.findByText("Outbound proxy settings saved"),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toHaveValue("");
  });

  it("sends a replacement password and clears it after a successful save", async () => {
    const user = userEvent.setup();
    view();
    await screen.findByDisplayValue("http://127.0.0.1:7890");
    await user.type(screen.getByLabelText("Password"), "new secret");
    await saveAndConfirm(user);
    await waitFor(() =>
      expect(updateEgressProxy).toHaveBeenCalledWith(
        expect.objectContaining({
          password: "new secret",
          clear_password: false,
          expected_revision: 4,
        }),
      ),
    );
    expect(screen.getByLabelText("Password")).toHaveValue("");
  });

  it("can explicitly clear the stored password", async () => {
    const user = userEvent.setup();
    view();
    await screen.findByDisplayValue("http://127.0.0.1:7890");
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Clear the saved password/ }),
    );
    await saveAndConfirm(user);
    await waitFor(() =>
      expect(updateEgressProxy).toHaveBeenCalledWith(
        expect.objectContaining({
          clear_password: true,
          expected_revision: 4,
        }),
      ),
    );
    expect(vi.mocked(updateEgressProxy).mock.calls[0][0]).not.toHaveProperty(
      "password",
    );
  });

  it("edits the enabled state, URL and username", async () => {
    const user = userEvent.setup();
    view();
    await screen.findByDisplayValue("http://127.0.0.1:7890");
    await user.click(
      screen.getByRole("switch", { name: "Use outbound proxy" }),
    );
    fireEvent.change(screen.getByLabelText("Proxy URL"), {
      target: { value: "socks5h://proxy.example:1080" },
    });
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "next-user" },
    });
    expect(screen.getByRole("switch")).not.toBeChecked();
    expect(screen.getByLabelText("Proxy URL")).toHaveValue(
      "socks5h://proxy.example:1080",
    );
    expect(screen.getByLabelText("Username")).toHaveValue("next-user");
  });

  it("keeps unsaved edits when the locale changes", async () => {
    const user = userEvent.setup();
    view({ withLocaleToggle: true });
    await screen.findByDisplayValue("http://127.0.0.1:7890");
    fireEvent.change(screen.getByLabelText("Username"), {
      target: { value: "unsaved-user" },
    });

    await user.click(screen.getByRole("button", { name: "Switch locale" }));

    expect(await screen.findByText("后台出站代理")).toBeInTheDocument();
    expect(getEgressProxy).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("用户名")).toHaveValue("unsaved-user");
  });

  it("shows the localized fallback when loading fails without an error object", async () => {
    vi.mocked(getEgressProxy).mockRejectedValueOnce("offline");
    view();
    expect(
      await screen.findByText("Outbound proxy settings are unavailable"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Save proxy settings" }),
    ).toBeDisabled();
  });

  it("shows a generic save failure and keeps the form available", async () => {
    vi.mocked(updateEgressProxy).mockRejectedValueOnce(
      new APIError("proxy storage offline", 500),
    );
    const user = userEvent.setup();
    view();
    await screen.findByDisplayValue("http://127.0.0.1:7890");
    await saveAndConfirm(user);
    expect(await screen.findByText("proxy storage offline")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Save proxy settings" }),
    ).toBeEnabled();
  });

  it("ignores a late load failure after unmount", async () => {
    let rejectLoad: (reason?: unknown) => void = () => undefined;
    vi.mocked(getEgressProxy).mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectLoad = reject;
        }),
    );
    const page = view();
    page.unmount();
    rejectLoad(new Error("late failure"));
    await Promise.resolve();
  });

  it("reloads after a revision conflict and allows a retry", async () => {
    const updated = {
      ...configured,
      proxy_url: "socks5h://proxy.example:1080",
      revision: 5,
    };
    vi.mocked(getEgressProxy)
      .mockResolvedValueOnce(configured)
      .mockResolvedValueOnce(updated);
    vi.mocked(updateEgressProxy)
      .mockRejectedValueOnce(new APIError("conflict", 409))
      .mockResolvedValueOnce({ ...updated, revision: 6 });
    const user = userEvent.setup();
    view();
    await screen.findByDisplayValue("http://127.0.0.1:7890");
    await saveAndConfirm(user);
    expect(
      await screen.findByText(/settings changed in another session/i),
    ).toBeInTheDocument();
    expect(
      screen.getByDisplayValue("socks5h://proxy.example:1080"),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Save proxy settings" }),
      ).toBeEnabled(),
    );
    await saveAndConfirm(user);
    await waitFor(() =>
      expect(vi.mocked(updateEgressProxy).mock.calls[1][0]).toEqual(
        expect.objectContaining({
          proxy_url: "socks5h://proxy.example:1080",
          expected_revision: 5,
        }),
      ),
    );
  });

  it("does not fetch or render proxy controls for a non-owner administrator", () => {
    state.role = "admin";
    const page = view();
    expect(page.container).toBeEmptyDOMElement();
    expect(getEgressProxy).not.toHaveBeenCalled();
  });

  it("tests the saved proxy through the confirmed owner flow", async () => {
    const user = userEvent.setup();
    view();
    await screen.findByDisplayValue("http://127.0.0.1:7890");
    await user.click(
      screen.getByRole("button", { name: "Test saved proxy" }),
    );
    await user.click(screen.getByRole("button", { name: "Confirm" }));
    expect(testEgressProxy).toHaveBeenCalledTimes(1);
    expect(
      await screen.findByText("Proxy connection succeeded in 42 ms"),
    ).toBeInTheDocument();
  });

  it("shows a probe failure without reporting success", async () => {
    vi.mocked(testEgressProxy).mockRejectedValueOnce(
      new APIError("egress proxy probe failed", 502),
    );
    const user = userEvent.setup();
    view();
    await screen.findByDisplayValue("http://127.0.0.1:7890");
    await user.click(
      screen.getByRole("button", { name: "Test saved proxy" }),
    );
    await user.click(screen.getByRole("button", { name: "Confirm" }));
    expect(
      await screen.findByText("egress proxy probe failed"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Proxy connection succeeded in 42 ms"),
    ).not.toBeInTheDocument();
  });
});
