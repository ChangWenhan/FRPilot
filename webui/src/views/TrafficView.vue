<template>
  <div>
    <h2>流量与带宽</h2>
    <p v-if="!snap" class="empty">加载中...</p>
    <template v-if="snap">
      <p v-if="snap.message" class="notice">{{ snap.message }}</p>
      <template v-if="snap.flows && snap.flows.length">
        <div class="cards">
          <div class="card"><div class="num">{{ fmtBytes(snap.totalIn) }}</div><div class="label">累计入流量</div></div>
          <div class="card"><div class="num">{{ fmtBytes(snap.totalOut) }}</div><div class="label">累计出流量</div></div>
          <div class="card"><div class="num small">{{ fmtBytes(snap.rateInSum) }}/s ↓</div><div class="label">实时入速率</div></div>
          <div class="card"><div class="num small">{{ fmtBytes(snap.rateOutSum) }}/s ↑</div><div class="label">实时出速率</div></div>
          <div class="card">
            <div class="num green small">{{ snap.top?.proxy }}</div>
            <div class="label">带宽主要流向</div>
          </div>
        </div>

        <div v-if="snap.anomalies && snap.anomalies.length" class="anomaly-bar">
          ⚠ 流量异常：{{ snap.anomalies.map(a => a.proxy).join('、') }} 出现速率突增
        </div>

        <div class="chart-wrap">
          <h3>带宽流向 Top（实时速率）</h3>
          <div ref="flowChart" class="chart"></div>
        </div>

        <section>
          <h3>各机器流量明细</h3>
          <div class="table-wrap"><table>
            <thead><tr><th>机器</th><th>累计入</th><th>累计出</th><th>入速率</th><th>出速率</th><th>入占比</th><th>状态</th></tr></thead>
            <tbody>
              <tr v-for="f in snap.flows" :key="f.proxy">
                <td><b>{{ f.proxy }}</b></td>
                <td>{{ fmtBytes(f.inBytes) }}</td>
                <td>{{ fmtBytes(f.outBytes) }}</td>
                <td>{{ fmtBytes(f.rateIn) }}/s</td>
                <td>{{ fmtBytes(f.rateOut) }}/s</td>
                <td>{{ f.pctIn }}%</td>
                <td><span v-if="f.anomaly" class="badge danger">突增</span><span v-else class="dim">正常</span></td>
              </tr>
            </tbody>
          </table></div>
        </section>

        <div class="chart-wrap">
          <h3>24 小时流量速率趋势 <button class="small ghost" @click="loadHistory">刷新</button></h3>
          <div ref="trendChart" class="chart tall"></div>
        </div>
      </template>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import * as echarts from 'echarts'
import { get, fmtBytes } from '../api.js'

const snap = ref(null)
const flowChart = ref(null)
const trendChart = ref(null)
let flowInst = null
let trendInst = null
let timer = null

onMounted(async () => {
  await load()
  timer = setInterval(load, 15000)
})
onUnmounted(() => {
  clearInterval(timer)
  flowInst?.dispose()
  trendInst?.dispose()
})

async function load() {
  snap.value = await get('/api/traffic')
  await nextTick()
  if (snap.value?.flows?.length) {
    drawFlow()
    loadHistory()
  }
}

async function loadHistory() {
  const r = await get('/api/traffic/history?hours=24')
  if (!r.points?.length) return
  // 按时间聚合所有 proxy 的入/出速率
  const byTs = new Map()
  for (const p of r.points) {
    const ts = p.ts.slice(0, 16)
    if (!byTs.has(ts)) byTs.set(ts, { in: 0, out: 0 })
    byTs.get(ts).in += p.rateIn
    byTs.get(ts).out += p.rateOut
  }
  const times = [...byTs.keys()]
  const inData = times.map(t => byTs.get(t).in)
  const outData = times.map(t => byTs.get(t).out)
  drawTrend(times, inData, outData)
}

function drawFlow() {
  if (!flowChart.value) return
  if (!flowInst) flowInst = echarts.init(flowChart.value)
  const sorted = [...snap.value.flows].sort((a, b) => (b.rateIn + b.rateOut) - (a.rateIn + a.rateOut))
  flowInst.setOption({
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, formatter: p => {
      const f = snap.value.flows.find(x => x.proxy === p[0].name)
      return `${f.proxy}<br/>入 ${fmtBytes(f.rateIn)}/s<br/>出 ${fmtBytes(f.rateOut)}/s<br/>累计入 ${fmtBytes(f.inBytes)} (${f.pctIn}%)`
    } },
    grid: { left: 120, right: 30, top: 10, bottom: 25 },
    xAxis: { type: 'value' },
    yAxis: { type: 'category', data: sorted.map(f => f.proxy) },
    series: [
      { name: '入', type: 'bar', stack: 't', data: sorted.map(f => f.rateIn), itemStyle: { color: '#38bdf8' } },
      { name: '出', type: 'bar', stack: 't', data: sorted.map(f => f.rateOut), itemStyle: { color: '#4ade80' } },
    ],
  })
}

function drawTrend(times, inData, outData) {
  if (!trendChart.value) return
  if (!trendInst) trendInst = echarts.init(trendChart.value)
  trendInst.setOption({
    tooltip: { trigger: 'axis', formatter: p => p.map(x => `${x.seriesName}: ${fmtBytes(x.value)}/s`).join('<br/>') },
    legend: { data: ['入速率', '出速率'], textStyle: { color: '#94a3b8' } },
    grid: { left: 60, right: 20, top: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { color: '#64748b' } },
    yAxis: { type: 'value', axisLabel: { color: '#64748b' } },
    series: [
      { name: '入速率', type: 'line', data: inData, smooth: true, showSymbol: false, itemStyle: { color: '#38bdf8' } },
      { name: '出速率', type: 'line', data: outData, smooth: true, showSymbol: false, itemStyle: { color: '#4ade80' } },
    ],
  })
}
</script>

<style scoped>
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 14px; margin: 14px 0; }
.card { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 16px; text-align: center; }
.num { font-size: 24px; font-weight: 700; color: #38bdf8; word-break: break-all; }
.num.small { font-size: 16px; }
.num.green { color: #4ade80; }
.label { color: #64748b; font-size: 12px; margin-top: 4px; }
.anomaly-bar { background: #7f1d1d55; border: 1px solid #7f1d1d; color: #f87171; padding: 10px 14px; border-radius: 8px; margin: 10px 0; }
.chart-wrap { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 16px; margin-top: 16px; }
.chart { width: 100%; height: 260px; }
.chart.tall { height: 300px; }
@media (max-width: 768px) { .chart { height: 220px; } .chart.tall { height: 260px; } }
h3 { margin: 0 0 10px; font-size: 15px; }
section { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 16px; margin-top: 16px; }
table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 8px; border-bottom: 1px solid #1e293b; font-size: 13px; }
th { color: #64748b; font-weight: 500; }
.badge.danger { background: #7f1d1d; color: #f87171; padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.dim { color: #64748b; }
.empty { color: #64748b; }
.notice { background: #78350f33; border: 1px solid #78350f; color: #fbbf24; padding: 10px 14px; border-radius: 8px; }
button.small { background: #334155; color: #cbd5e1; border: none; border-radius: 5px; padding: 4px 10px; font-size: 12px; cursor: pointer; }
</style>
