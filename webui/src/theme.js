const THEME_KEY = 'frpilot_theme'
export const THEME_EVENT = 'themechange'

export function currentTheme() {
  let t = 'dark'
  try { t = localStorage.getItem(THEME_KEY) || 'dark' } catch {}
  return t === 'light' ? 'light' : 'dark'
}

export function applyTheme(t) {
  document.documentElement.setAttribute('data-theme', t)
}

// 挂载前调用，避免首屏闪错主题
export function initTheme() {
  applyTheme(currentTheme())
  window.addEventListener('storage', (e) => {
    if (e.key === THEME_KEY && e.newValue) {
      applyTheme(e.newValue)
      document.dispatchEvent(new CustomEvent(THEME_EVENT))
    }
  })
}

export function toggleTheme() {
  const t = currentTheme() === 'dark' ? 'light' : 'dark'
  try { localStorage.setItem(THEME_KEY, t) } catch {}
  applyTheme(t)
  document.dispatchEvent(new CustomEvent(THEME_EVENT))
  return t
}

// 图表等需要在主题切换后重渲染的组件读取 CSS 变量
export function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}
