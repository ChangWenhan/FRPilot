<template>
  <div>
    <div class="page-head">
      <h2>机器</h2>
      <span class="sub">共 {{ machines.length }} 台 · 每 15 秒自动刷新</span>
      <span class="grow" />
      <button v-if="isAdmin" class="btn btn--ghost btn--sm" @click="discover">重新扫描 frps</button>
    </div>
    <p v-if="discoverMsg" class="msg msg--ok">{{ discoverMsg }}</p>

    <div v-if="status" class="stat-grid">
      <div class="stat"><div class="v">{{ status.machines.total }}</div><div class="l">机器总数</div></div>
      <div class="stat"><div class="v v--ok">{{ status.machines.enabled }}</div><div class="l">监控中</div></div>
      <div class="stat"><div class="v v--warn">{{ status.machines.pending }}</div><div class="l">待配置</div></div>
      <div class="stat"><div class="v">{{ status.frps?.clients ?? '-' }}</div><div class="l">frps 在线客户端</div></div>
      <div class="stat">
        <div class="v v--sm">{{ fmtBytes(status.frps?.trafficIn) }} / {{ fmtBytes(status.frps?.trafficOut) }}</div>
        <div class="l">累计入 / 出流量</div>
      </div>
    </div>

    <div class="card" style="margin-top: 16px">
      <div class="card-head">
        <h3>token 基线</h3>
        <code>{{ status?.tokenSet ? status?.tokenMask : '未设置' }}</code>
        <span class="badge badge--ok">已保护，不回显原文</span>
      </div>
    </div>

    <div class="card" style="margin-top: 16px">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th style="width: 52px" class="num">ID</th>
              <th>名称</th>
              <th class="num">隧道端口</th>
              <th>状态</th>
              <th>SSH 凭据</th>
              <th>最近在线</th>
              <th style="width: 90px" class="num">操作</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="m in machines" :key="m.id">
              <tr>
                <td class="num muted-cell">{{ m.id }}</td>
                <td>
                  <b class="truncate d-block" :title="m.name">{{ m.name }}</b>
                </td>
                <td class="num">{{ m.tunnelPort || '-' }}</td>
                <td><span class="badge" :class="badgeCls(m.status)">{{ statusText[m.status] }}</span></td>
                <td>
                  <span :title="m.hasCredentials ? `${m.sshUser}${m.hasSudo ? ' +sudo' : ''}` : ''">
                    {{ m.hasCredentials ? `${m.sshUser}${m.hasSudo ? ' +sudo' : ''}` : '未配置' }}
                  </span>
                </td>
                <td class="muted-cell">{{ m.lastSeenAt ? fmtTs(m.lastSeenAt) : '-' }}</td>
                <td>
                  <div class="td-actions">
                    <router-link class="btn btn--ghost btn--sm btn--icon" :to="`/machines/${m.id}`" title="详情">
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                    </router-link>
                    <ActionMenu v-if="isAdmin" :items="menuItems(m)" @action="k => onAction(k, m)" />
                  </div>
                </td>
              </tr>
              <tr v-if="editId === m.id" class="edit-row">
                <td colspan="7">
                  <div class="edit-panel">
                    <div class="edit-fields">
                      <div class="field">
                        <label>SSH 用户</label>
                        <input class="input" v-model="credUser" placeholder="如 root" autocomplete="off" />
                      </div>
                      <div class="field">
                        <label>SSH 密码</label>
                        <input class="input" v-model="credPass" type="password" placeholder="留空保持旧密码" autocomplete="new-password" />
                      </div>
                      <div class="field">
                        <label>sudo 密码 <span class="hint">执行 root 命令时使用</span></label>
                        <input class="input" v-model="sudoPass" type="password" placeholder="留空保持旧密码" autocomplete="new-password" />
                      </div>
                    </div>
                    <div class="edit-actions">
                      <button class="btn btn--ghost btn--sm" @click="editId = null">取消</button>
                      <button class="btn btn--sm" @click="saveCreds(m)">保存凭据</button>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-if="!machines.length">
              <td colspan="7" class="empty-row">
                尚未发现机器。请先在「设置」配置 frps 连接信息，然后点击「重新扫描 frps」。
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { get, post, statusText, fmtBytes } from '../api.js'
import ActionMenu from '../components/ActionMenu.vue'

const machines = ref([])
const status = ref(null)
const editId = ref(null)
const credUser = ref('')
const credPass = ref('')
const sudoPass = ref('')
const isAdmin = ref(false)
const discoverMsg = ref('')
let timer = null

onMounted(async () => {
  const me = await get('/api/auth/me')
  isAdmin.value = me.role === 'admin'
  await load()
  timer = setInterval(load, 15000)
})
onUnmounted(() => clearInterval(timer))

async function load() {
  status.value = await get('/api/status')
  const r = await get('/api/machines')
  machines.value = r.machines
}

async function discover() {
  discoverMsg.value = ''
  const r = await post('/api/machines/discover')
  const news = (r.newFound || []).join('、')
  discoverMsg.value = `扫描完成：共 ${r.total} 个隧道${news ? '，新增：' + news : ''}`
  await load()
}

function badgeCls(s) {
  return { pending: 'badge--warn', configured: 'badge--accent', enabled: 'badge--ok', disabled: 'badge' }[s] || ''
}
function fmtTs(t) { return new Date(t).toLocaleString() }

function menuItems(m) {
  return [
    { key: 'edit', label: m.hasCredentials ? '编辑凭据' : '配置凭据', icon: pencilIcon() },
    { key: m.enabled ? 'disable' : 'enable', label: m.enabled ? '停用监控' : (m.hasCredentials ? '启用监控' : '待配置凭据'), icon: m.enabled ? stopIcon() : playIcon(), disabled: !m.hasCredentials && !m.enabled },
    { key: 'detail', label: '查看详情', icon: eyeIcon() },
  ]
}
function onAction(k, m) {
  if (k === 'edit') startEdit(m)
  else if (k === 'enable' || k === 'disable') toggle(m)
  else if (k === 'detail') location.href = `/machines/${m.id}`
}

function startEdit(m) {
  editId.value = m.id
  credUser.value = m.sshUser || ''
  credPass.value = ''
  sudoPass.value = ''
}

async function saveCreds(m) {
  await post(`/api/machines/${m.id}/credentials`, { sshUser: credUser.value, sshPass: credPass.value, sudoPass: sudoPass.value })
  editId.value = null
  await load()
}

async function toggle(m) {
  await post(`/api/machines/${m.id}/enable`, { enabled: !m.enabled })
  await load()
}

function pencilIcon() { return '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17 3a2.8 2.8 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z"/></svg>' }
function playIcon() { return '<svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg>' }
function stopIcon() { return '<svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="1.5"/></svg>' }
function eyeIcon() { return '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>' }
</script>

<style scoped>
.truncate { display: block; max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.d-block { display: block; }
.td-actions { display: flex; align-items: center; justify-content: flex-end; gap: 6px; }
.edit-row td { background: var(--accent-tint); }
.edit-panel { display: flex; flex-direction: column; gap: 12px; padding: 4px 0; }
.edit-fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; }
.edit-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
