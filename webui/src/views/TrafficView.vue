<template>
  <div>
    <div class="page-head">
      <h2>流量与带宽</h2>
      <span class="sub">每 15 秒自动刷新</span>
    </div>

    <div v-if="isAdmin" class="card qos-card">
      <div class="card-head"><h3>带宽控制</h3><span class="dim">frps 服务器 tc 整形，按机器限速，不改任何 frpc 配置、不踢会话</span></div>
      <div class="card-body">
        <div class="field" style="max-width: 360px">
          <label>模式</label>
          <select class="select" v-model="form.qos.mode">
            <option value="off">关闭</option>
            <option value="auto">自动均衡</option>
            <option value="manual">手动限速</option>
          </select>
        </div>

        <template v-if="form.qos.mode === 'auto'">
          <div class="field-grid" style="margin-top: 14px">
            <div class="field">
              <label>出站预算 Mbps</label>
              <input class="input" type="number" min="0" step="0.1" v-model.number="form.qos.budgetOutMbps" />
            </div>
            <div class="field">
              <label>入站预算 Mbps</label>
              <input class="input" type="number" min="0" step="0.1" v-model.number="form.qos.budgetInMbps" />
            </div>
            <div class="field">
              <label>活跃判定阈值 KB/s</label>
              <input class="input" type="number" min="1" v-model.number="form.qos.activeKBps" />
            </div>
            <div class="field">
              <label>滞后窗口（秒）</label>
              <input class="input" type="number" min="15" v-model.number="form.qos.hysteresisSec" />
            </div>
            <div class="field">
              <label>整形接口</label>
              <input class="input" v-model="form.qos.interface" placeholder="留空自动检测" />
            </div>
          </div>
          <p class="hint" style="margin-top: 10px">速率超过阈值的机器视为正在被操作，不管开几个连接都算一台；出站预算按活跃机器数平均分配并严格封顶，全部空闲时自动不限速。出站预算填服务器实际带宽即可，入站预算留空表示该方向不限制。</p>
        </template>

        <template v-if="form.qos.mode === 'manual'">
          <div class="manual-grid" style="margin-top: 14px">
            <template v-for="m in machines" :key="m.name">
              <span class="manual-name" :title="m.name">{{ m.name }}</span>
              <div class="field"><label>出站 Mbps</label>
                <input class="input" type="number" min="0" step="0.1" v-model.number="manualMap[m.name].out" placeholder="留空不限" />
              </div>
              <div class="field"><label>入站 Mbps</label>
                <input class="input" type="number" min="0" step="0.1" v-model.number="manualMap[m.name].in" placeholder="留空不限" />
              </div>
            </template>
          </div>
          <p class="hint" style="margin-top: 10px">填写的机器按固定额度限速，留空的机器不限速。</p>
        </template>

        <div class="action-bar" style="margin-top: 14px">
          <button class="btn" @click="saveQos">保存带宽控制</button>
          <span v-if="qosMsg" class="hint">{{ qosMsg }}</span>
        </div>
      </div>
    </div>

    <p v-if="!snap" class="empty">加载中...</p>
    <template v-if="snap">
      <p v-if="snap.message" class="notice">{{ snap.message }}</p>
      <template v-if="snap.flows && snap.flows.length">
        <div class="stat-grid">
          <div class="stat"><div class="v">{{ fmtBytes(snap.totalIn) }}</div><div class="l">累计入流量</div></div>
          <div class="stat"><div class="v">{{ fmtBytes(snap.totalOut) }}</div><div class="l">累计出流量</div></div>
          <div class="stat"><div class="v v--ok v--sm">{{ fmtBytes(snap.rateInSum) }}/s ↓</div><div class="l">实时入速率</div></div>
          <div class="stat"><div class="v v--ok v--sm">{{ fmtBytes(snap.rateOutSum) }}/s ↑</div><div class="l">实时出速率</div></div>
          <div class="stat">
            <div class="v v--warn v--sm truncate" :title="snap.top?.proxy">{{ snap.top?.proxy || '-' }}</div>
            <div class="l">带宽主要流向</div>
          </div>
        </div>

        <div v-if="snap.anomalies && snap.anomalies.length" class="notice">
          <b>⚠ 流量异常：</b>{{ snap.anomalies.map(a => a.proxy).join('、') }} 出现速率突增
        </div>

        <div class="chart-box">
          <h3>带宽流向 Top（实时速率）</h3>
          <div ref="flowChart" class="chart"></div>
        </div>

        <div class="card mt-4">
          <div class="card-head">
            <h3>各机器流量明细</h3>
            <span class="grow" />
            <span class="dim">{{ snap.flows.length }} 台</span>
          </div>
          <div class="table-wrap">
            <table class="table">
              <thead>
                <tr>
                  <th>机器</th><th class="num">累计入</th><th class="num">累计出</th>
                  <th class="num">入速率</th><th class="num">出速率</th><th class="num">入占比</th><th>状态</th><th class="num">出站额度</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="f in snap.flows" :key="f.proxy">
                  <td><b class="truncate d-block" :title="f.proxy">{{ f.proxy }}</b></td>
                  <td class="num">{{ fmtBytes(f.inBytes) }}</td>
                  <td class="num">{{ fmtBytes(f.outBytes) }}</td>
                  <td class="num">{{ fmtBytes(f.rateIn) }}/s</td>
                  <td class="num">{{ fmtBytes(f.rateOut) }}/s</td>
                  <td class="num">{{ f.pctIn }}%</td>
                  <td>
                    <span v-if="f.anomaly" class="badge badge--danger">突增</span>
                    <span v-else class="badge badge--ok">正常</span>
                  </td>
                  <td class="num"><span :class="quotaCls(f.proxy)">{{ quotaText(f.proxy) }}</span></td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div class="chart-box">
          <div class="chart-head">
            <h3>24 小时流量速率趋势</h3>
            <button class="btn btn--ghost btn--sm" @click="loadHistory">刷新</button>
          </div>
          <div ref="trendChart" class="chart chart--tall"></div>
        </div>
      </template>
      <p v-else class="empty">暂无流量数据（frps 未配置或隧道无流量）</p>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, nextTick } from 'vue'
import * as echarts from 'echarts'
import { get, post, fmtBytes } from '../api.js'
import { cssVar, THEME_EVENT } from '../theme.js'

const snap = ref(null)
const qosStat = ref(null)
const isAdmin = ref(false)
const machines = ref([])
const manualMap = reactive({})
const form = reactive({ qos: {} })
const qosMsg = ref('')
const flowChart = ref(null)
const trendChart = ref(null)
let flowInst = null
let trendInst = null
let timer = null

onMounted(async () => {
  await load()
  timer = setInterval(load, 15000)
  document.addEventListener(THEME_EVENT, redraw)
  try {
    const me = await get('/api/auth/me')
    isAdmin.value = me.role === 'admin'
    if (isAdmin.value) await loadQosForm()
  } catch {}
})
onUnmounted(() => {
  clearInterval(timer)
  document.removeEventListener(THEME_EVENT, redraw)
  flowInst?.dispose()
  trendInst?.dispose()
})

// 主题切换后按新配色重绘两张图
function redraw() {
  if (snap.value?.flows?.length) {
    drawFlow()
    loadHistory()
  }
}

async function load() {
  snap.value = await get('/api/traffic')
  try { qosStat.value = await get('/api/qos/status') } catch {}
  await nextTick()
  if (snap.value?.flows?.length) {
    drawFlow()
    loadHistory()
  }
}

// 带宽控制出站额度展示（- 表示未启用；不限 = 该机器无固定额度）
function quotaText(proxy) {
  const q = qosStat.value
  if (!q || q.mode === 'off') return '-'
  const kbps = (q.quotaOutKbps || {})[proxy]
  if (!kbps) return '不限'
  return (kbps / 1000).toFixed(1) + 'M'
}
function quotaCls(proxy) {
  const q = qosStat.value
  if (!q || q.mode === 'off') return 'muted-cell'
  return (q.active || []).includes(proxy) ? '' : 'muted-cell'
}

// ---- 带宽控制（管理员） ----

async function loadQosForm() {
  try {
    const s = await get('/api/settings')
    Object.assign(form.qos, {
      mode: s.qos?.mode || 'off',
      budgetOutMbps: s.qos?.budgetOutMbps || 0,
      budgetInMbps: s.qos?.budgetInMbps || 0,
      activeKBps: s.qos?.activeKBps || 1,
      hysteresisSec: s.qos?.hysteresisSec || 45,
      interface: s.qos?.interface || '',
    })
    for (const it of s.qos?.manual || []) {
      if (!manualMap[it.name]) manualMap[it.name] = { out: 0, in: 0 }
      manualMap[it.name].out = it.outMbps || 0
      manualMap[it.name].in = it.inMbps || 0
    }
    const r = await get('/api/machines')
    machines.value = (r.machines || []).map(m => ({ name: m.name, id: m.id }))
    machines.value.forEach(m => {
      if (!manualMap[m.name]) manualMap[m.name] = { out: 0, in: 0 }
    })
  } catch {}
}

async function saveQos() {
  try {
    const manual = Object.entries(manualMap)
      .filter(([name, v]) => name && (v.out > 0 || v.in > 0))
      .map(([name, v]) => ({ name, outMbps: v.out || 0, inMbps: v.in || 0 }))
    await post('/api/settings', { qos: { ...form.qos, manual } })
    qosMsg.value = '已保存并生效'
    setTimeout(() => { qosMsg.value = '' }, 3000)
    try { qosStat.value = await get('/api/qos/status') } catch {}
  } catch (e) {
    qosMsg.value = e.message
  }
}

async function loadHistory() {
  const r = await get('/api/traffic/history?hours=24')
  if (!r.points?.length) return
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
    xAxis: { type: 'value', axisLabel: { color: cssVar('--muted-2'), formatter: v => fmtBytes(v) + '/s' } },
    yAxis: { type: 'category', data: sorted.map(f => f.proxy), axisLabel: { color: cssVar('--muted'), overflow: 'truncate', width: 100 } },
    series: [
      { name: '入', type: 'bar', stack: 't', data: sorted.map(f => f.rateIn), itemStyle: { color: cssVar('--accent') } },
      { name: '出', type: 'bar', stack: 't', data: sorted.map(f => f.rateOut), itemStyle: { color: cssVar('--ok') } },
    ],
  })
}

function drawTrend(times, inData, outData) {
  if (!trendChart.value) return
  if (!trendInst) trendInst = echarts.init(trendChart.value)
  trendInst.setOption({
    tooltip: { trigger: 'axis', formatter: p => p.map(x => `${x.seriesName}: ${fmtBytes(x.value)}/s`).join('<br/>') },
    legend: { data: ['入速率', '出速率'], textStyle: { color: cssVar('--muted') } },
    grid: { left: 60, right: 20, top: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { color: cssVar('--muted-2') } },
    yAxis: { type: 'value', axisLabel: { color: cssVar('--muted-2'), formatter: v => fmtBytes(v) + '/s' } },
    series: [
      { name: '入速率', type: 'line', data: inData, smooth: true, showSymbol: false, itemStyle: { color: cssVar('--accent') } },
      { name: '出速率', type: 'line', data: outData, smooth: true, showSymbol: false, itemStyle: { color: cssVar('--ok') } },
    ],
  })
}
</script>

<style scoped>
.truncate { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.d-block { display: block; }
.chart-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.chart-head h3 { margin: 0; font-size: 14.5px; font-weight: 600; }
.qos-card { margin-bottom: var(--sp-4); }
.manual-grid { display: grid; grid-template-columns: 1fr 160px 160px; gap: 12px; align-items: end; }
.manual-name { font-size: 13.5px; font-weight: 550; padding-bottom: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 900px) {
  .manual-grid { grid-template-columns: 1fr 1fr; }
  .manual-name { grid-column: 1 / -1; padding-bottom: 0; }
}
</style>
