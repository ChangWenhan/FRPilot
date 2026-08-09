<template>
  <div class="action-menu" ref="root">
    <button class="btn btn--ghost btn--sm btn--icon" type="button" aria-label="操作菜单" @click.stop="open = !open">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <circle cx="5" cy="12" r="1.8" />
        <circle cx="12" cy="12" r="1.8" />
        <circle cx="19" cy="12" r="1.8" />
      </svg>
    </button>
    <div v-if="open" class="menu" role="menu">
      <button
        v-for="it in items"
        :key="it.label"
        type="button"
        role="menuitem"
        class="menu-item"
        :class="{ 'menu-item--danger': it.danger, 'menu-item--disabled': it.disabled }"
        :disabled="it.disabled"
        @click="pick(it)"
      >
        <span v-if="it.icon" class="mi" v-html="it.icon"></span>
        <span>{{ it.label }}</span>
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'

const props = defineProps({
  items: { type: Array, required: true },
})
const emit = defineEmits(['action'])

const open = ref(false)
const root = ref(null)

function pick(it) {
  open.value = false
  emit('action', it.key || it.label)
}

function onDocClick(e) {
  if (root.value && !root.value.contains(e.target)) open.value = false
}
function onKey(e) {
  if (e.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKey)
})
</script>

<style scoped>
.action-menu { position: relative; display: inline-block; }
.menu {
  position: absolute;
  right: 0;
  top: calc(100% + 4px);
  z-index: 50;
  min-width: 168px;
  padding: 4px;
  background: var(--surface-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  box-shadow: 0 12px 28px rgba(0, 0, 0, .45);
}
.menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  height: 32px;
  padding: 0 10px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--fg);
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}
.menu-item:hover:not(:disabled) { background: rgba(56, 189, 248, .1); }
.menu-item--danger { color: var(--danger); }
.menu-item--danger:hover:not(:disabled) { background: rgba(248, 113, 113, .1); }
.menu-item--disabled { color: var(--muted-2); cursor: not-allowed; }
.mi { display: inline-flex; width: 16px; justify-content: center; }
</style>
