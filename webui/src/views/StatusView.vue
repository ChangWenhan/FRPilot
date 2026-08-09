<template>
  <div>
    <div class="page-head">
      <h2>总览</h2>
      <span class="sub">每 15 秒自动刷新</span>
    </div>

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
      <div class="card-head">
        <h3>机器状态</h3>
        <span class="grow" />
        <span class="dim">{{ machines.length }} 台</span>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>名称</th>
              <th class="num">隧道端口</th>
              <th>状态</th>
              <th>凭据</th>
              <th>最近在线</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in machines" :key="m.id">
              <td><span class="truncate d-block" :title="m.name">{{ m.name }}</span></td>
              <td class="num">{{ m.tunnelPort || '-' }}</td>
              <td><span class="badge" :class="badgeCls(m.status)">{{ statusText[m.status] }}</span></td>
              <td class="muted-cell">{{ m.hasCredentials ? '已配置' : '未配置' }}</td>
              <td class="muted-cell">{{ m.lastSeenAt ? fmtTs(m.lastSeenAt) : '-' }}</td>
            </tr>
            <tr v-if="!machines.length"><td colspan="5" class="empty-row">暂无机器数据</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { get, fmtBytes, statusText } from '../api.js'

const status = ref(null)
const machines = ref([])

onMounted(async () => {
  await load()
  setInterval(load, 15000)
})

async function load() {
  status.value = await get('/api/status')
  const r = await get('/api/machines')
  machines.value = r.machines
}

function badgeCls(s) {
  return { pending: 'badge--warn', configured: 'badge--accent', enabled: 'badge--ok', disabled: '' }[s] || ''
}
function fmtTs(t) { return new Date(t).toLocaleString() }
</script>

<style scoped>
.truncate { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.d-block { display: block; }
</style>
