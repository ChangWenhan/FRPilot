<template>
  <button
    type="button"
    class="btn btn--ghost btn--sm btn--icon theme-btn"
    :title="theme === 'light' ? '切换到深色模式' : '切换到浅色模式'"
    @click="onToggle"
  >
    <svg v-if="theme === 'dark'" width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="4" fill="currentColor" />
      <path d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    </svg>
    <svg v-else width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" fill="currentColor" />
    </svg>
  </button>
</template>

<script setup>
import { ref } from 'vue'
import { currentTheme, toggleTheme } from '../theme.js'

const theme = ref(currentTheme())

function onToggle() {
  theme.value = toggleTheme()
}
</script>

<style scoped>
.theme-btn { color: var(--muted); }
.theme-btn:hover:not(:disabled) { color: var(--fg); }
</style>
