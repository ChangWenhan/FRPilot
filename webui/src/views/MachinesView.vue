<template>
  <div>
    <div class="head">
      <h2>机器管理</h2>
      <button v-if="isAdmin" @click="discover">重新扫描 frps</button>
    </div>
    <p v-if="discoverMsg" class="ok">{{ discoverMsg }}</p>
    <div class="table-wrap"><table>
      <thead>
        <tr><th>ID</th><th>名称</th><th>隧道端口</th><th>状态</th><th>SSH 凭据</th><th>监控开关</th></tr>
      </thead>
      <tbody>
        <tr v-for="m in machines" :key="m.id">
          <td>{{ m.id }}</td>
          <td><b>{{ m.name }}</b></td>
          <td>{{ m.tunnelPort }}</td>
          <td><span class="badge" :class="m.status">{{ statusText[m.status] }}</span></td>
          <td>
            <template v-if="editId === m.id">
              <input v-model="credUser" placeholder="SSH 用户" class="inline" />
              <input v-model="credPass" type="password" placeholder="SSH 密码" class="inline" />
              <button class="small ok" @click="saveCreds(m)">保存</button>
              <button class="small ghost" @click="editId = null">取消</button>
            </template>
            <template v-else>
              <span>{{ m.hasCredentials ? `已配置 (${m.sshUser})` : '未配置' }}</span>
              <button v-if="isAdmin" class="small ghost" @click="startEdit(m)">编辑</button>
            </template>
          </td>
          <td>
            <button v-if="isAdmin" :class="['small', m.enabled ? 'danger' : 'ok']"
              @click="toggle(m)">
              {{ m.enabled ? '停用监控' : (m.hasCredentials ? '启用监控' : '待配置凭据') }}
            </button>
            <span v-else>{{ m.enabled ? '监控中' : '未启用' }}</span>
            <a class="small link" :href="`/machines/${m.id}`">详情 →</a>
          </td>
        </tr>
        <tr v-if="!machines.length">
          <td colspan="6" class="empty">
            尚未发现机器。请先在「设置」配置 frps 连接信息，然后点击「重新扫描 frps」。
          </td>
        </tr>
      </tbody>
    </table></div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { get, post, statusText } from '../api.js'

const machines = ref([])
const editId = ref(null)
const credUser = ref('')
const credPass = ref('')
const isAdmin = ref(false)
const discoverMsg = ref('')

onMounted(async () => {
  const me = await get('/api/auth/me')
  isAdmin.value = me.role === 'admin'
  await load()
})

async function load() {
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

function startEdit(m) {
  editId.value = m.id
  credUser.value = m.sshUser || ''
  credPass.value = ''
}

async function saveCreds(m) {
  await post(`/api/machines/${m.id}/credentials`, { sshUser: credUser.value, sshPass: credPass.value })
  editId.value = null
  await load()
}

async function toggle(m) {
  await post(`/api/machines/${m.id}/enable`, { enabled: !m.enabled })
  await load()
}
</script>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: center; }
table { width: 100%; border-collapse: collapse; margin-top: 12px; }
@media (max-width: 768px) {
  .inline { width: 90px; }
  .head h2 { font-size: 18px; }
}
th, td { text-align: left; padding: 10px; border-bottom: 1px solid #1e293b; font-size: 14px; }
th { color: #64748b; font-weight: 500; }
.inline { width: 110px; margin-right: 6px; background: #0f172a; border: 1px solid #334155; border-radius: 5px; padding: 6px 8px; color: #e2e8f0; }
.badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; background: #334155; color: #94a3b8; }
.badge.enabled { background: #14532d; color: #4ade80; }
.badge.pending { background: #78350f; color: #fbbf24; }
.badge.disabled { background: #374151; color: #9ca3af; }
.badge.configured { background: #1e3a8a; color: #93c5fd; }
button.small { padding: 5px 10px; font-size: 12px; margin-left: 6px; border-radius: 5px; border: none; cursor: pointer; }
.ok { background: #0ea5e9; color: #fff; }
.ok:hover { background: #0284c7; }
.danger { background: #ef4444; color: #fff; }
.ghost { background: #334155; color: #cbd5e1; }
.empty { text-align: center; color: #64748b; padding: 32px; }
.ok { color: #4ade80; }
a.link { color: #38bdf8; text-decoration: none; margin-left: 8px; font-size: 12px; }
a.link:hover { text-decoration: underline; }
</style>
