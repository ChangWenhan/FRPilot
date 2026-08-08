<template>
  <div class="auth-card">
    <h1>FRPilot</h1>
    <p class="sub">FRP 全栈监控平台</p>
    <div class="tabs">
      <button :class="{ on: mode === 'login' }" @click="mode = 'login'">登录</button>
      <button :class="{ on: mode === 'register' }" @click="mode = 'register'">注册</button>
    </div>
    <form @submit.prevent="submit">
      <input v-model="username" placeholder="用户名" autocomplete="username" />
      <input v-model="password" type="password" placeholder="密码" autocomplete="current-password" />
      <input v-if="mode === 'register'" v-model="confirm" type="password" placeholder="确认密码" />
      <p v-if="error" class="error">{{ error }}</p>
      <button type="submit" class="primary">{{ mode === 'login' ? '登录' : '注册' }}</button>
    </form>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { post, saveToken } from '../api.js'

const mode = ref('login')
const username = ref('')
const password = ref('')
const confirm = ref('')
const error = ref('')

async function submit() {
  error.value = ''
  if (mode.value === 'register') {
    if (password.value !== confirm.value) {
      error.value = '两次输入的密码不一致'
      return
    }
    try {
      const r = await post('/api/auth/register', { username: username.value, password: password.value })
      alert(r.message || '注册成功')
      mode.value = 'login'
      return
    } catch (e) {
      error.value = e.message
      return
    }
  }
  try {
    const r = await post('/api/auth/login', { username: username.value, password: password.value })
    if (r.token) saveToken(r.token)
    // 整页刷新：让 App.vue 重新挂载并加载用户信息
    window.location.href = '/'
  } catch (e) {
    error.value = e.message
  }
}
</script>

<style scoped>
.auth-card {
  max-width: 360px; margin: 12vh auto; padding: 32px;
  background: #1e293b; border-radius: 12px; border: 1px solid #334155;
}
h1 { margin: 0; text-align: center; color: #38bdf8; font-size: 26px; }
.sub { text-align: center; color: #64748b; margin: 6px 0 20px; font-size: 13px; }
.tabs { display: flex; gap: 8px; margin-bottom: 16px; }
.tabs button { flex: 1; background: #334155; }
.tabs button.on { background: #0ea5e9; }
form { display: flex; flex-direction: column; gap: 12px; }
input {
  background: #0f172a; border: 1px solid #334155; border-radius: 6px;
  padding: 10px 12px; color: #e2e8f0; font-size: 14px;
}
.error { color: #f87171; font-size: 13px; margin: 0; }
.primary { padding: 10px; font-size: 15px; }
@media (max-width: 768px) { .auth-card { margin: 8vh 16px; padding: 24px; } }
</style>
