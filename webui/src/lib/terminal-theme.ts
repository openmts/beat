export interface TerminalTheme {
  background: string
  foreground: string
  cursor: string
  cursorAccent: string
  selectionBackground: string
  black: string
  red: string
  green: string
  yellow: string
  blue: string
  magenta: string
  cyan: string
  white: string
  brightBlack: string
  brightRed: string
  brightGreen: string
  brightYellow: string
  brightBlue: string
  brightMagenta: string
  brightCyan: string
  brightWhite: string
}

// The WebShell palettes are anchored to the app's neutral shadcn theme so the
// terminal blends with the surrounding UI in both light and dark modes.
export const darkTerminalTheme: TerminalTheme = {
  background: "#0a0a0a",
  foreground: "#fafafa",
  cursor: "#e5e5e5",
  cursorAccent: "#0a0a0a",
  selectionBackground: "#3f3f3f",
  black: "#1f1f1f",
  red: "#e06c75",
  green: "#98c379",
  yellow: "#d19a66",
  blue: "#61afef",
  magenta: "#c678dd",
  cyan: "#56b6c2",
  white: "#d4d4d4",
  brightBlack: "#6b6b6b",
  brightRed: "#f08a92",
  brightGreen: "#b5d69a",
  brightYellow: "#e5bf84",
  brightBlue: "#8cc7f5",
  brightMagenta: "#d69be8",
  brightCyan: "#7fd3dd",
  brightWhite: "#fafafa",
}

export const lightTerminalTheme: TerminalTheme = {
  background: "#ffffff",
  foreground: "#1a1a1a",
  cursor: "#333333",
  cursorAccent: "#ffffff",
  selectionBackground: "#d4d4d4",
  black: "#1f1f1f",
  red: "#c7424b",
  green: "#4c8a3f",
  yellow: "#a46b2a",
  blue: "#2f6fb2",
  magenta: "#9a4fa8",
  cyan: "#2d8a92",
  white: "#525252",
  brightBlack: "#6b6b6b",
  brightRed: "#d96a73",
  brightGreen: "#64a557",
  brightYellow: "#c38b48",
  brightBlue: "#4f8ec4",
  brightMagenta: "#b26cc0",
  brightCyan: "#4ba4ac",
  brightWhite: "#333333",
}
