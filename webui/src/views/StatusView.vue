<template>
  <div>
    <h2>总览</h2>
    <div v-if="status" class="cards">
      <div class="card">
        <div class="num">{{ status.machines.total }}</div>
        <div class="label">机器总数</div>
      </div>
      <div class="card">
        <div class="num green">{{ status.machines.enabled }}</div>
        <div class="label">监控中</div>
      </div>
      <div class="card">
        <div class="num amber">{{ status.machines.pending }}</div>
        <div class="label">待配置</div>
      </div>
      <div class="card" v-if="status.frps">
        <div class="num">{{ status.frps.clients }}</div>
        <div class="label">frps 在线客户端</div>
      </div>
      <div class="card" v-if="status.frps">
        <div class="num small">{{ fmtBytes(status.frps.trafficIn) }} / {{ fmtBytes(status.frps.trafficOut) }}</div>
        <div class="label">入 / 出流量（累计）</div>
      </div>
    </div>
    <div class="token-bar">
      token 基线 <code>{{ status?.tokenBaseline }}</code>
      <span class="badge">只读，禁止修改</span>
    </div>
    <h3>机器状态</h3>
    <div class="table-wrap"><table>
      <thead><tr><th>名称</th><th>隧道端口</th><th>状态</th><th>凭据</th><th>最近在线</th></tr></thead>
      <tbody>
        <tr v-for="m in machines" :key="m.id">
          <td>{{ m.name }}</td>
          <td>{{ m.tunnelPort }}</td>
          <td><span class="badge" :class="m.status">{{ statusText[m.status] }}</span></td>
          <td>{{ m.hasCredentials ? '已配置' : '未配置' }}</td>
          <td>{{ m.lastSeenAt ? new Date(m.lastSeenAt).toLocaleString() : '-' }}</td>
        </tr>
      </tbody>
    </table></div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { get, fmtBytes, statusText } from '../api.js'

const status = ref(null)
const machines = ref([])

onMounted(async () => {
  status.value = await get('/api/status')
  const r = await get('/api/machines')
  machines.value = r.machines
  setInterval(async () => {
    status.value = await get('/api/status')
    const r2 = await get('/api/machines')
    machines.value = r2.machines
  }, 15000)
})
</script>

<style scoped>
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 16px; margin: 16px 0; }
.card { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 18px; text-align: center; }
.num { font-size: 30px; font-weight: 700; color: #38bdf8; }
.num.small { font-size: 18px; }
.num.green { color: #4ade80; }
.num.amber { color: #fbbf24; }
.label { color: #64748b; font-size: 12px; margin-top: 4px; }
.token-bar { background: #1e293b; border: 1px solid #334155; border-radius: 8px; padding: 10px 14px; font-size: 13px; }
code { background: #0f172a; padding: 2px 8px; border-radius: 4px; color: #4ade80; }
.badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; background: #334155; color: #94a3b8; margin-left: 6px; }
.badge.enabled { background: #14532d; color: #4ade80; }
.badge.pending { background: #78350f; color: #fbbf24; }
.badge.disabled { background: #374151; color: #9ca3af; }
.badge.configured { background: #1e3a8a; color: #93c5fd; }
h3 { color: #94a3b8; margin-top: 28px; }
table { width: 100%; border-collapse: collapse; margin-top: 8px; }
@media (max-width: 768px) { .num { font-size: 24px; } }
th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid #1e293b; font-size: 14px; }
th { color: #64748b; font-weight: 500; }
</style>
