export function publicIdle(): boolean {
 const element = document.activeElement
 return document.visibilityState === "visible" && !document.querySelector('[role="dialog"]') && (!element || element === document.body || Boolean(element.closest("[data-public-pwa]"))) && !window.getSelection()?.toString()
}
