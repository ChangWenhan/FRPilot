<template>
  <div class="auth-wrap">
    <div class="auth-card">
      <div class="brand">
        <svg width="30" height="30" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path d="M13 2 3 14h6l-1 8 10-12h-6l1-8z" fill="currentColor" />
        </svg>
        <span>FRPilot</span>
      </div>
      <p class="sub">FRP 全栈监控平台</p>
      <div class="tabs">
        <button type="button" class="tab" :class="{ on: mode === 'login' }" @click="mode = 'login'">登录</button>
        <button type="button" class="tab" :class="{ on: mode === 'register' }" @click="mode = 'register'">注册</button>
      </div>
      <form @submit.prevent="submit">
        <div class="field">
          <label for="username">用户名</label>
          <input id="username" class="input" v-model="username" placeholder="请输入用户名" autocomplete="username" autofocus />
        </div>
        <div class="field">
          <label for="password">密码</label>
          <input id="password" class="input" v-model="password" type="password" placeholder="请输入密码" autocomplete="current-password" />
        </div>
        <div v-if="mode === 'register'" class="field">
          <label for="confirm">确认密码</label>
          <input id="confirm" class="input" v-model="confirm" type="password" placeholder="再次输入密码" autocomplete="new-password" />
        </div>
        <p v-if="error" class="msg msg--err">{{ error }}</p>
        <button type="submit" class="btn btn--block" :disabled="submitting">
          {{ submitting ? '请稍候...' : (mode === 'login' ? '登录' : '注册') }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { post } from '../api.js'

const mode = ref('login')
const username = ref('')
const password = ref('')
const confirm = ref('')
const error = ref('')
const submitting = ref(false)

async function submit() {
  error.value = ''
  if (mode.value === 'register') {
    if (password.value !== confirm.value) {
      error.value = '两次输入的密码不一致'
      return
    }
    submitting.value = true
    try {
      const r = await post('/api/auth/register', { username: username.value, password: password.value })
      alert(r.message || '注册成功')
      mode.value = 'login'
      return
    } catch (e) {
      error.value = e.message
    } finally {
      submitting.value = false
    }
    return
  }
  submitting.value = true
  try {
    await post('/api/auth/login', { username: username.value, password: password.value })
    // 整页刷新：让 App.vue 重新挂载并加载用户信息
    window.location.href = '/'
  } catch (e) {
    error.value = e.message
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.auth-wrap { min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 16px; }
.auth-card {
  width: 100%;
  max-width: 380px;
  padding: 32px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 14px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, .4);
}
.brand { display: flex; align-items: center; justify-content: center; gap: 10px; color: var(--accent); font-size: 24px; font-weight: 700; }
.sub { text-align: center; color: var(--muted); margin: 6px 0 20px; font-size: 13px; }
form { display: flex; flex-direction: column; gap: 14px; }
.tabs { display: flex; gap: 2px; padding: 4px; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--radius); margin-bottom: 18px; }
.tab { flex: 1; height: 34px; border: none; border-radius: var(--radius-sm); background: transparent; color: var(--muted); font: inherit; font-size: 13.5px; cursor: pointer; }
.tab.on { background: var(--surface-3); color: var(--fg); font-weight: 600; }
@media (max-width: 768px) { .auth-card { padding: 24px; } }
</style>
