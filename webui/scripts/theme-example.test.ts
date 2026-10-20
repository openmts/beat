import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { afterEach, describe, expect, it, vi } from "vitest"

// 主题示例在 webui 之外，且 jsdom 下的动态 import 会被 vitest 拦截，
// 这里读取已发布源码、去掉模块语法后在 jsdom 全局执行，验证的是真实交付物。
const themeDir = resolve(process.cwd(), "../themes/pulse/dist")
const html = readFileSync(resolve(themeDir, "index.html"), "utf8")

type Connect = (render: (snapshot: ReturnType<typeof snapshot>) => void) => { destroy: () => void }

function loadTheme(): void {
	const sdk = readFileSync(resolve(themeDir, "beat-sdk.js"), "utf8").replace(/^export /m, "")
	const connect = new Function(`${sdk}\nreturn connectBeatTheme`)() as Connect
	const source = readFileSync(resolve(themeDir, "theme.js"), "utf8").replace(/^\s*import .*$/m, "")
	void new Function("connectBeatTheme", source)(connect)
}

function hostMessage(beat: unknown, origin = location.origin): Event {
	const event = new Event("message")
	Object.defineProperty(event, "source", { value: window })
	Object.defineProperty(event, "origin", { value: origin })
	Object.defineProperty(event, "data", { value: { beat } })
	return event
}

function snapshot() {
	return {
		timestamp: "2026-10-14T04:00:00Z",
		locale: "en",
		site: { site_title: "Beat Fleet", site_description: "Edge status" },
		groups: [],
		nodes: [
			{
				id: "node-one", name: "edge-gw-01", alias: "Edge one", group_id: "group-one", status: "online",
				tags: ["edge", "prod"], public_remark: "Primary gateway", telemetry_state: "fresh", telemetry_age_seconds: 3,
				metrics: { cpu: 42, memory: 61, disk: 95 },
			},
			{ id: "node-two", name: "cache-01", group_id: "group-one", status: "offline", tags: [], metrics: { cpu: 0, memory: 0, disk: 0 } },
		],
		state: { nodes: "ready", metrics: "ready", network: "ready" },
	}
}

describe("shipped sandbox theme example", () => {
	afterEach(() => { document.body.innerHTML = ""; vi.restoreAllMocks() })

	it("is a sandboxed package entry that renders a public snapshot through the bridge", () => {
		document.body.innerHTML = html.slice(html.indexOf("<body>") + 6, html.indexOf("</body>"))
		const posts: unknown[] = []
		vi.spyOn(window, "postMessage").mockImplementation((message) => { posts.push(message) })
		loadTheme()

		const instance = "a".repeat(32)
		window.dispatchEvent(hostMessage({ version: 2, instance_id: instance, type: "init" }))
		expect(posts).toContainEqual({ beat: { version: 2, instance_id: instance, type: "ready" } })

		window.dispatchEvent(hostMessage({ version: 2, instance_id: instance, type: "snapshot", payload: snapshot() }))
		expect(document.getElementById("site-title")?.textContent).toBe("Beat Fleet")
		expect(document.getElementById("online-count")?.textContent).toBe("1")
		expect(document.getElementById("offline-count")?.textContent).toBe("1")
		expect(document.getElementById("node-count")?.textContent).toBe("2 public nodes")
		expect(document.querySelectorAll(".pulse-card")).toHaveLength(2)

		const first = document.querySelector<HTMLButtonElement>(".pulse-open")
		expect(first?.textContent).toBe("Edge one")
		first?.click()
		expect(posts).toContainEqual({ beat: { version: 2, instance_id: instance, type: "navigate", payload: { path: "/node/node-one" } } })

		const fills = [...document.querySelectorAll<HTMLElement>(".pulse-fill")].map((fill) => fill.dataset.level)
		expect(fills.slice(0, 3)).toEqual(["ok", "ok", "bad"])
		expect(document.getElementById("status")?.textContent).toBe("")
	})

	it("reports loading and error states instead of zero values", () => {
		document.body.innerHTML = html.slice(html.indexOf("<body>") + 6, html.indexOf("</body>"))
		const posts: unknown[] = []
		vi.spyOn(window, "postMessage").mockImplementation((message) => { posts.push(message) })
		loadTheme()
		const instance = "b".repeat(32)
		window.dispatchEvent(hostMessage({ version: 2, instance_id: instance, type: "init" }))

		const empty = { ...snapshot(), nodes: [], state: { nodes: "loading", metrics: "loading", network: "ready" } }
		window.dispatchEvent(hostMessage({ version: 2, instance_id: instance, type: "snapshot", payload: empty }))
		expect(document.getElementById("status")?.textContent).toBe("Loading public data…")
		expect(document.querySelector(".pulse-empty")?.textContent).toBe("Loading public data…")

		const failed = { ...snapshot(), nodes: [], state: { nodes: "ready", metrics: "error", network: "ready" } }
		window.dispatchEvent(hostMessage({ version: 2, instance_id: instance, type: "snapshot", payload: failed }))
		expect(document.getElementById("status")?.dataset.state).toBe("error")
		expect(document.getElementById("status")?.textContent).toBe("Public data is temporarily unavailable.")
		expect(posts.some((message) => (message as { beat?: { type?: string } }).beat?.type === "error")).toBe(false)
	})
})
