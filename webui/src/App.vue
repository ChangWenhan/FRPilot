<template>
  <div class="shell">
    <nav v-if="me">
      <span class="brand">FRPilot</span>
      <router-link to="/">总览</router-link>
      <router-link to="/machines">机器</router-link>
      <router-link to="/traffic">流量</router-link>
      <router-link to="/actions">操作</router-link>
      <router-link v-if="me.role === 'admin'" to="/settings">设置</router-link>
      <span class="spacer" />
      <span class="user">{{ me.username }} ({{ me.role === 'admin' ? '管理员' : '普通用户' }})</span>
      <button @click="logout">退出</button>
    </nav>
    <main><router-view /></main>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { get, post, clearToken } from './api.js'

const me = ref(null)
const router = useRouter()

onMounted(async () => {
  // 登录页不校验身份（避免未登录时无限 401 刷新循环）
  if (router.currentRoute.value.path === '/login') return
  try {
    me.value = await get('/api/auth/me')
  } catch {
    // api.js 已跳转登录页
  }
})

async function logout() {
  try { await post('/api/auth/logout') } catch {}
  clearToken()
  window.location.href = '/login'
}
</script>

<style>
/* 全局响应式基础 */
:root {
  --nav-h: 52px;
}
* { box-sizing: border-box; }
html, body { margin: 0; }
body { background: #0f172a; color: #e2e8f0; font-family: system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; }
#app { min-height: 100vh; }
/* 表格移动端横向滚动容器 */
.table-wrap { width: 100%; overflow-x: auto; -webkit-overflow-scrolling: touch; }
.table-wrap table { min-width: 640px; }
.table-wrap-compact table { min-width: 420px; }
/* 移动端断点 */
@media (max-width: 768px) {
  :root { --nav-h: 46px; }
  main { padding: 14px !important; }
  h2 { font-size: 18px !important; }
}
</style>

<style scoped>
.shell { min-height: 100vh; }
nav {
  display: flex; align-items: center; gap: 12px;
  padding: 0 16px; height: var(--nav-h);
  background: #1e293b; border-bottom: 1px solid #334155;
  overflow-x: auto; /* 窄屏横向滚动导航 */
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
}
nav::-webkit-scrollbar { display: none; }
nav a { color: #94a3b8; text-decoration: none; padding: 4px 8px; border-radius: 6px; white-space: nowrap; font-size: 14px; }
nav a.router-link-active { color: #fff; background: #334155; }
.brand { font-weight: 700; color: #38bdf8; white-space: nowrap; font-size: 15px; }
.spacer { flex: 1; min-width: 8px; }
.user { color: #94a3b8; font-size: 12px; white-space: nowrap; display: none; }
@media (min-width: 768px) { .user { display: inline; } }
button {
  background: #0ea5e9; color: #fff; border: none; border-radius: 6px;
  padding: 6px 14px; cursor: pointer; font-size: 13px;
}
button:hover { background: #0284c7; }
button.danger { background: #ef4444; }
button.danger:hover { background: #dc2626; }
button.ghost { background: #334155; }
button.ghost:hover { background: #475569; }
main { padding: 24px; max-width: 1100px; margin: 0 auto; }
</style>
