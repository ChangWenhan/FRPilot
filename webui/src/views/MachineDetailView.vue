<template>
  <div v-if="machine">
    <div class="head">
      <button class="ghost" @click="$router.push('/machines')">← 返回</button>
      <h2>{{ machine.name }}</h2>
      <span class="badge" :class="machine.status">{{ statusText[machine.status] }}</span>
      <span v-if="data?.tunnelUp === false" class="badge danger">隧道不通</span>
      <span class="spacer" />
      <span v-if="data" class="ts">采集于 {{ fmtTs(data.ts) }}</span>
      <button class="ghost" @click="collectNow">立即采集</button>
    </div>

    <p v-if="machine.status !== 'enabled'" class="notice">
      该机器未启用监控。请先在「机器」页填写 SSH 凭据并启用监控，才能查看采集数据。
    </p>

    <div v-if="data">
      <div class="cards">        <div class="card"><div class="num" :class="pctClass(data?.sys?.cpuPct)">{{ pct(data?.sys?.cpuPct) }}</div><div class="label">CPU 使用率</div></div>
        <div class="card"><div class="num" :class="pctClass(memPct)">{{ pct(memPct) }}</div><div class="label">内存使用率</div></div>
        <div class="card"><div class="num" :class="pctClass(diskPct)">{{ pct(diskPct) }}</div><div class="label">磁盘使用率（最大）</div></div>
        <div class="card">
          <template v-if="gpu?.present">
            <div class="num">{{ pct(gpu.util) }}</div>
            <div class="label">GPU {{ gpu.name }} · {{ gpu.tempC }}°C · {{ gpu.memUsedMiB }}/{{ gpu.memTotalMiB }}MiB</div>
          </template>
          <template v-else>
            <div class="num dim">-</div><div class="label">无 GPU</div>
          </template>
        </div>
        <div class="card"><div class="num small">{{ fmtBytes(data?.sys?.netInRateBps) }}/s ↓</div><div class="label">入网速率</div></div>
        <div class="card"><div class="num small">{{ fmtBytes(data?.sys?.netOutRateBps) }}/s ↑</div><div class="label">出网速率</div></div>
      </div>

      <div class="meta-line">
        主机名 {{ data.hostname }} · {{ data.sys?.os }} · 内核 {{ data.sys?.kernel }} · {{ data.sys?.cpuCores }} 核 ·
        负载 {{ data.sys?.load1 }}/{{ data.sys?.load5 }}/{{ data.sys?.load15 }} · 运行 {{ fmtUptime(data.sys?.uptimeSec) }}
      </div>

      <div class="chart-wrap">
        <h3>24 小时历史趋势</h3>
        <div ref="trendChart" class="chart"></div>
      </div>

      <section>
        <h3>磁盘</h3>
        <div class="table-wrap"><table>
          <thead><tr><th>挂载点</th><th>容量</th><th>已用</th><th>使用率</th></tr></thead>
          <tbody>
            <tr v-for="d in data.sys?.disk || []" :key="d.mount">
              <td>{{ d.mount }}</td>
              <td>{{ d.sizeGB }} GB</td>
              <td>{{ d.usedGB }} GB</td>
              <td><span class="badge" :class="pctClass(d.usePct)">{{ pct(d.usePct) }}</span></td>
            </tr>
          </tbody>
        </table></div>
      </section>

      <section>
        <h3>安全软件</h3>
        <div class="table-wrap"><table>
          <thead><tr><th>软件</th><th>安装</th><th>服务状态</th><th>详情</th></tr></thead>
          <tbody>
            <tr v-for="it in data.security || []" :key="it.name">
              <td>{{ it.name }}</td>
              <td>{{ it.installed ? '已安装' : '未安装' }}</td>
              <td>
                <span v-if="!it.installed" class="badge warn">未安装</span>
                <span v-else-if="it.active === 'active'" class="badge ok">运行中</span>
                <span v-else-if="it.active === 'inactive'" class="badge danger">未运行</span>
                <span v-else class="badge">{{ it.active }}</span>
              </td>
              <td class="dim">{{ it.version || it.detail || '' }}</td>
            </tr>
            <tr v-if="!(data.security || []).length"><td colspan="4" class="empty">暂无安全软件数据</td></tr>
          </tbody>
        </table></div>
      </section>

      <section>
        <h3>定时任务 <span class="count">{{ (data.cron || []).length }} 条</span></h3>
        <div class="table-wrap"><table>
          <thead><tr><th>来源</th><th>用户</th><th>任务</th></tr></thead>
          <tbody>
            <tr v-for="(c, i) in data.cron || []" :key="i">
              <td class="dim">{{ c.source }}</td>
              <td>{{ c.user }}</td>
              <td class="mono">{{ c.line }}</td>
            </tr>
            <tr v-if="!(data.cron || []).length"><td colspan="3" class="empty">暂无定时任务</td></tr>
          </tbody>
        </table></div>
      </section>

      <section>
        <h3>端口开放 <span class="count">{{ (data.ports || []).length }} 个</span></h3>
        <div class="table-wrap"><table>
          <thead><tr><th>端口</th><th>进程</th></tr></thead>
          <tbody>
            <tr v-for="(p, i) in data.ports || []" :key="i">
              <td>{{ p.port }}</td>
              <td>{{ p.process }}</td>
            </tr>
            <tr v-if="!(data.ports || []).length"><td colspan="2" class="empty">暂无端口数据</td></tr>
          </tbody>
        </table></div>
      </section>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import * as echarts from 'echarts'
import { get, post, fmtBytes, statusText } from '../api.js'

const route = useRoute()
const machine = ref(null)
const data = ref(null)
const trendChart = ref(null)
let trendInst = null
let timer = null

onMounted(async () => {
  await load()
  timer = setInterval(load, 15000)
})
onUnmounted(() => {
  clearInterval(timer)
  trendInst?.dispose()
})

async function load() {
  const r = await get(`/api/machines/${route.params.id}/snapshot`)
  machine.value = r.machine
  data.value = r.data
  await nextTick()
  if (data.value) loadMetrics()
}

async function loadMetrics() {
  const r = await get(`/api/machines/${route.params.id}/metrics?hours=24`)
  if (!r.points?.length) return
  const times = r.points.map(p => new Date(p.ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }))
  const cpu = r.points.map(p => p.cpu >= 0 ? p.cpu : null)
  const mem = r.points.map(p => p.mem >= 0 ? p.mem : null)
  const disk = r.points.map(p => p.disk >= 0 ? p.disk : null)
  const gpu = r.points.map(p => p.gpuUtil >= 0 ? p.gpuUtil : null)
  if (!trendChart.value) return
  if (!trendInst) trendInst = echarts.init(trendChart.value)
  trendInst.setOption({
    tooltip: { trigger: 'axis', formatter: p => p.map(x => `${x.seriesName}: ${x.value == null ? '-' : x.value.toFixed(1) + '%'}`).join('<br/>') },
    legend: { data: ['CPU', '内存', '磁盘', 'GPU'], textStyle: { color: '#94a3b8' } },
    grid: { left: 50, right: 20, top: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { color: '#64748b', fontSize: 10 } },
    yAxis: { type: 'value', max: 100, axisLabel: { color: '#64748b' } },
    series: [
      { name: 'CPU', type: 'line', data: cpu, smooth: true, showSymbol: false, itemStyle: { color: '#38bdf8' } },
      { name: '内存', type: 'line', data: mem, smooth: true, showSymbol: false, itemStyle: { color: '#4ade80' } },
      { name: '磁盘', type: 'line', data: disk, smooth: true, showSymbol: false, itemStyle: { color: '#fbbf24' } },
      { name: 'GPU', type: 'line', data: gpu, smooth: true, showSymbol: false, itemStyle: { color: '#f472b6' } },
    ],
  })
}

async function collectNow() {
  await post(`/api/machines/${route.params.id}/collect-now`)
  await load()
}

const memPct = computed(() => {
  const s = data.value?.sys
  if (!s || !s.memTotalMB) return -1
  return (s.memTotalMB - s.memAvailMB) / s.memTotalMB * 100
})
const diskPct = computed(() => {
  const ds = data.value?.sys?.disk || []
  return Math.max(-1, ...ds.map(d => d.usePct))
})
const gpu = computed(() => data.value?.gpu)

function pct(v) { return v == null || v < 0 ? '-' : Math.round(v) + '%' }
function pctClass(v) { return v != null && v >= 85 ? 'danger' : v != null && v >= 70 ? 'warn' : 'normal' }
function fmtTs(t) { return t ? new Date(t).toLocaleString() : '-' }
function fmtUptime(sec) {
  if (sec == null) return '-'
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60)
  return d ? `${d}天${h}时` : `${h}时${m}分`
}
</script>

<style scoped>
.head { display: flex; align-items: center; gap: 10px; }
.head h2 { margin: 0; font-size: 20px; }
.spacer { flex: 1; }
.ts { color: #64748b; font-size: 12px; }
.notice { background: #78350f33; border: 1px solid #78350f; color: #fbbf24; padding: 10px 14px; border-radius: 8px; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 14px; margin: 14px 0; }
.card { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 16px; text-align: center; }
.num { font-size: 26px; font-weight: 700; }
.num.normal { color: #38bdf8; }
.num.warn { color: #fbbf24; }
.num.danger { color: #ef4444; }
.num.dim { color: #475569; }
.num.small { font-size: 17px; }
.label { color: #64748b; font-size: 12px; margin-top: 4px; }
.meta-line { color: #94a3b8; font-size: 13px; margin: 10px 0; }
section { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 16px; margin-top: 16px; }
h3 { margin: 0 0 10px; font-size: 15px; }
.count { color: #64748b; font-size: 12px; font-weight: 400; }
table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 7px 8px; border-bottom: 1px solid #1e293b; font-size: 13px; }
th { color: #64748b; font-weight: 500; }
.dim { color: #64748b; }
.mono { font-family: ui-monospace, monospace; font-size: 12px; }
.empty { text-align: center; color: #475569; padding: 20px; }
.badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; background: #334155; color: #94a3b8; }
.badge.ok { background: #14532d; color: #4ade80; }
.badge.warn { background: #78350f; color: #fbbf24; }
.badge.danger { background: #7f1d1d; color: #f87171; }
.badge.enabled { background: #14532d; color: #4ade80; }
.chart-wrap { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 16px; margin-top: 16px; }
.chart { width: 100%; height: 260px; }
@media (max-width: 768px) { .chart { height: 220px; } .head h2 { font-size: 18px; } }
.chart-wrap h3 { margin: 0 0 10px; font-size: 15px; }
button.ghost { background: #334155; color: #cbd5e1; border: none; border-radius: 6px; padding: 7px 14px; cursor: pointer; font-size: 13px; }
button.ghost:hover { background: #475569; }
</style>
