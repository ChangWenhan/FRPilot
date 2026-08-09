<template>
  <div v-if="machine">
    <div class="page-head">
      <button class="btn btn--ghost btn--sm" @click="$router.push('/machines')">← 返回</button>
      <h2 class="truncate" :title="machine.name">{{ machine.name }}</h2>
      <span class="badge" :class="badgeCls(machine.status)">{{ statusText[machine.status] }}</span>
      <span v-if="data?.tunnelUp === false" class="badge badge--danger">隧道不通</span>
      <span class="grow" />
      <span v-if="data" class="dim small">{{ fmtTs(data.ts) }}</span>
      <button class="btn btn--ghost btn--sm" @click="collectNow">立即采集</button>
    </div>

    <p v-if="machine.status !== 'enabled'" class="notice">
      该机器未启用监控。请先在「机器」页填写 SSH 凭据并启用监控，才能查看采集数据。
    </p>

    <div v-if="data">
      <div class="stat-grid">
        <div class="stat"><div class="v" :class="pctColor(cpuPct)">{{ pct(cpuPct) }}</div><div class="l">CPU 使用率</div></div>
        <div class="stat"><div class="v" :class="pctColor(memPct)">{{ pct(memPct) }}</div><div class="l">内存使用率</div></div>
        <div class="stat"><div class="v" :class="pctColor(diskPct)">{{ pct(diskPct) }}</div><div class="l">磁盘使用率（最大）</div></div>
        <div class="stat">
          <template v-if="gpu?.present">
            <div class="v" :class="pctColor(gpu.util)">{{ pct(gpu.util) }}</div>
            <div class="l">GPU · {{ gpu.tempC }}°C · {{ gpu.memUsedMiB }}/{{ gpu.memTotalMiB }}MiB</div>
          </template>
          <template v-else>
            <div class="v v--muted">-</div><div class="l">无 GPU</div>
          </template>
        </div>
        <div class="stat"><div class="v v--sm">{{ fmtBytes(data?.sys?.netInRateBps) }}/s ↓</div><div class="l">入网速率</div></div>
        <div class="stat"><div class="v v--sm">{{ fmtBytes(data?.sys?.netOutRateBps) }}/s ↑</div><div class="l">出网速率</div></div>
      </div>

      <div class="meta-bar mono">
        主机名 {{ data.hostname }} · {{ data.sys?.os }} · 内核 {{ data.sys?.kernel }} · {{ data.sys?.cpuCores }} 核 · 负载 {{ data.sys?.load1 }}/{{ data.sys?.load5 }}/{{ data.sys?.load15 }} · 运行 {{ fmtUptime(data.sys?.uptimeSec) }}
      </div>

      <div class="chart-box">
        <h3>24 小时历史趋势</h3>
        <div ref="trendChart" class="chart"></div>
      </div>

      <div class="card" style="margin-top: 16px">
        <div class="card-head"><h3>磁盘</h3></div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th>挂载点</th><th class="num">容量</th><th class="num">已用</th><th class="num">使用率</th></tr></thead>
            <tbody>
              <tr v-for="d in data.sys?.disk || []" :key="d.mount">
                <td class="mono-cell" :title="d.mount">{{ d.mount }}</td>
                <td class="num">{{ d.sizeGB }} GB</td>
                <td class="num">{{ d.usedGB }} GB</td>
                <td class="num"><span class="badge" :class="pctColor(d.usePct)">{{ pct(d.usePct) }}</span></td>
              </tr>
              <tr v-if="!(data.sys?.disk || []).length"><td colspan="4" class="empty-row">暂无磁盘数据</td></tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h3>安全软件</h3></div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th>软件</th><th>安装</th><th>服务状态</th><th>详情</th></tr></thead>
            <tbody>
              <tr v-for="it in data.security || []" :key="it.name">
                <td><b>{{ it.name }}</b></td>
                <td class="muted-cell">{{ it.installed ? '已安装' : '未安装' }}</td>
                <td>
                  <span v-if="!it.installed" class="badge badge--warn">未安装</span>
                  <span v-else-if="it.active === 'active'" class="badge badge--ok">运行中</span>
                  <span v-else-if="it.active === 'inactive'" class="badge badge--danger">未运行</span>
                  <span v-else class="badge">{{ it.active }}</span>
                </td>
                <td class="muted-cell truncate" :title="it.version || it.detail || ''">{{ it.version || it.detail || '-' }}</td>
              </tr>
              <tr v-if="!(data.security || []).length"><td colspan="4" class="empty-row">暂无安全软件数据</td></tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <h3>定时任务</h3>
          <span class="badge badge--accent">{{ (data.cron || []).length }} 条</span>
        </div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th>来源</th><th>用户</th><th>任务</th></tr></thead>
            <tbody>
              <tr v-for="(c, i) in data.cron || []" :key="i">
                <td class="muted-cell">{{ c.source }}</td>
                <td>{{ c.user }}</td>
                <td class="mono-cell" :title="c.line">{{ c.line }}</td>
              </tr>
              <tr v-if="!(data.cron || []).length"><td colspan="3" class="empty-row">暂无定时任务</td></tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <h3>端口开放</h3>
          <span class="badge badge--accent">{{ (data.ports || []).length }} 个</span>
        </div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th class="num">端口</th><th>进程</th></tr></thead>
            <tbody>
              <tr v-for="(p, i) in data.ports || []" :key="i">
                <td class="num mono-cell">{{ p.port }}</td>
                <td class="mono-cell" :title="p.process">{{ p.process }}</td>
              </tr>
              <tr v-if="!(data.ports || []).length"><td colspan="2" class="empty-row">暂无端口数据</td></tr>
            </tbody>
          </table>
        </div>
      </div>
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
    legend: { data: ['CPU', '内存', '磁盘', 'GPU'], textStyle: { color: '#8b9cbd' } },
    grid: { left: 50, right: 20, top: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { color: '#5f7194', fontSize: 10 } },
    yAxis: { type: 'value', max: 100, axisLabel: { color: '#5f7194' } },
    series: [
      { name: 'CPU', type: 'line', data: cpu, smooth: true, showSymbol: false, itemStyle: { color: '#38bdf8' } },
      { name: '内存', type: 'line', data: mem, smooth: true, showSymbol: false, itemStyle: { color: '#34d399' } },
      { name: '磁盘', type: 'line', data: disk, smooth: true, showSymbol: false, itemStyle: { color: '#fbbf24' } },
      { name: 'GPU', type: 'line', data: gpu, smooth: true, showSymbol: false, itemStyle: { color: '#f472b6' } },
    ],
  })
}

async function collectNow() {
  await post(`/api/machines/${route.params.id}/collect-now`)
  await load()
}

const cpuPct = computed(() => data.value?.sys?.cpuPct ?? -1)
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
function pctColor(v) { return v != null && v >= 85 ? 'v--danger' : v != null && v >= 70 ? 'v--warn' : '' }
function badgeCls(s) {
  return { pending: 'badge--warn', configured: 'badge--accent', enabled: 'badge--ok', disabled: '' }[s] || ''
}
function fmtTs(t) { return t ? new Date(t).toLocaleString() : '-' }
function fmtUptime(sec) {
  if (sec == null) return '-'
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60)
  return d ? `${d}天${h}时` : `${h}时${m}分`
}
</script>

<style scoped>
.truncate { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.meta-bar {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 10px 14px;
  margin: var(--sp-4) 0 0;
  font-size: 12.5px;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
