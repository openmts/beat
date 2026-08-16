import { describe, expect, it } from "vitest"
import { darkTerminalTheme, lightTerminalTheme, type TerminalTheme } from "@/lib/terminal-theme"

const requiredKeys: (keyof TerminalTheme)[] = [
  "background", "foreground", "cursor", "cursorAccent", "selectionBackground",
  "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
  "brightBlack", "brightRed", "brightGreen", "brightYellow",
  "brightBlue", "brightMagenta", "brightCyan", "brightWhite",
]

describe("terminal themes", () => {
  it("defines every required color in both palettes", () => {
    for (const theme of [darkTerminalTheme, lightTerminalTheme]) {
      for (const key of requiredKeys) {
        expect(theme[key], `missing ${key}`).toMatch(/^#[0-9a-fA-F]{6}$/)
      }
    }
  })

  it("keeps dark and light backgrounds distinct", () => {
    expect(darkTerminalTheme.background).not.toBe(lightTerminalTheme.background)
    expect(darkTerminalTheme.foreground).not.toBe(lightTerminalTheme.foreground)
  })
})
