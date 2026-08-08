const TOKEN_KEY = 'frpmon_token'

export function saveToken(t) {
  localStorage.setItem(TOKEN_KEY, t)
}
export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}
function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

async function api(method, path, body) {
  const headers = {}
  if (body) headers['Content-Type'] = 'application/json'
  const token = getToken()
  if (token) headers['Authorization'] = 'Bearer ' + token
  const resp = await fetch(path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  })
  const data = await resp.json().catch(() => ({}))
  if (resp.status === 401 && path !== '/api/auth/login') {
    clearToken()
    // 已在登录页时不再跳转，避免无限循环刷新
    if (window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
    const e = new Error('未登录')
    e.status = 401
    throw e
  }
  if (!resp.ok) {
    const e = new Error(data.error || `HTTP ${resp.status}`)
    e.status = resp.status
    throw e
  }
  return data
}

export const get = (p) => api('GET', p)
export const post = (p, b) => api('POST', p, b)
export const del = (p) => api('DELETE', p)
export const put = (p, b) => api('PUT', p, b)

export function fmtBytes(n) {
  if (n == null) return '-'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return n.toFixed(i === 0 ? 0 : 1) + ' ' + u[i]
}

export const statusText = {
  pending: '待配置',
  configured: '已配置',
  enabled: '监控中',
  disabled: '停用',
}
