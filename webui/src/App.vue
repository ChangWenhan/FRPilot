<template>
  <div class="shell">
    <nav v-if="me">
      <span class="brand">FRPilot</span>
      <div class="nav-links">
        <router-link to="/machines">机器</router-link>
        <router-link to="/traffic">流量</router-link>
        <router-link to="/actions">操作</router-link>
        <router-link v-if="me.role === 'admin'" to="/settings">设置</router-link>
      </div>
      <span class="grow" />
      <span class="user"><i class="dot"></i>{{ me.username }} · {{ me.role === 'admin' ? '管理员' : '普通用户' }}</span>
      <button class="btn btn--ghost btn--sm btn--icon theme-btn" :title="theme === 'light' ? '切换到深色模式' : '切换到浅色模式'" @click="onToggleTheme">
        <svg v-if="theme === 'dark'" width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <circle cx="12" cy="12" r="4" fill="currentColor" />
          <path d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>
        <svg v-else width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" fill="currentColor" />
        </svg>
      </button>
      <button class="btn btn--ghost btn--sm" @click="logout">退出</button>
    </nav>
    <main><router-view /></main>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { get, post, clearToken } from './api.js'
import { currentTheme, toggleTheme } from './theme.js'

const me = ref(null)
const theme = ref(currentTheme())
const router = useRouter()

function onToggleTheme() {
  theme.value = toggleTheme()
}

onMounted(async () => {
  if (router.currentRoute.value.path === '/login') return
  try {
    me.value = await get('/api/auth/me')
  } catch {}
})

async function logout() {
  try { await post('/api/auth/logout') } catch {}
  clearToken()
  window.location.href = '/login'
}
</script>

<style scoped>
.shell { min-height: 100vh; }
nav {
  display: flex;
  align-items: center;
  gap: 16px;
  height: var(--nav-h);
  padding: 0 18px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  z-index: 40;
}
.brand { font-weight: 700; color: var(--accent); font-size: 15px; white-space: nowrap; }
.nav-links { display: flex; align-items: center; gap: 2px; overflow-x: auto; scrollbar-width: none; }
.nav-links::-webkit-scrollbar { display: none; }
.nav-links a {
  color: var(--muted);
  text-decoration: none;
  padding: 6px 12px;
  border-radius: 7px;
  font-size: 14px;
  white-space: nowrap;
  transition: all .12s ease;
}
.nav-links a:hover { color: var(--fg); text-decoration: none; }
.nav-links a.router-link-active { color: var(--fg); background: var(--surface-3); font-weight: 550; }
.grow { flex: 1; min-width: 8px; }
.user { display: flex; align-items: center; gap: 6px; color: var(--muted); font-size: 12.5px; white-space: nowrap; }
.theme-btn { color: var(--muted); display: inline-flex; align-items: center; justify-content: center; }
.theme-btn:hover { color: var(--fg); }
.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--ok); display: inline-block; }
main { padding: 22px 24px 40px; max-width: 1140px; margin: 0 auto; }
@media (max-width: 768px) {
  nav { gap: 8px; padding: 0 10px; }
  .user { display: none; }
  main { padding: 14px 12px 32px; }
}
</style>
