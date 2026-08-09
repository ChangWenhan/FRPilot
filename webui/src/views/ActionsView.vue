<template>
  <div>
    <div class="page-head">
      <h2>操作中心</h2>
      <span class="grow" />
      <span class="dim small">root 权限操作自动通过已配置的 sudo 密码执行</span>
    </div>

    <div class="tabs">
      <button type="button" class="tab" :class="{ on: tab === 'cleanup' }" @click="tab = 'cleanup'">一键清理</button>
      <button type="button" class="tab" :class="{ on: tab === 'health' }" @click="tab = 'health'">一键体检</button>
      <button type="button" class="tab" :class="{ on: tab === 'tasks' }" @click="tab = 'tasks'">执行记录</button>
    </div>

    <!-- 一键清理 -->
    <div v-if="tab === 'cleanup'">
      <div class="card">
        <div class="card-head"><h3>选择机器</h3></div>
        <div class="card-body">
          <div class="choice-list">
            <label v-for="m in machines" :key="m.id" class="choice-item" :class="{ on: cleanupMachines.includes(m.id) }">
              <input type="checkbox" :value="m.id" v-model="cleanupMachines" :disabled="!m.hasCredentials" />
              <span class="truncate">{{ m.name }}</span>
              <span v-if="!m.hasCredentials" class="meta">未配置凭据</span>
              <span v-else-if="m.enabled" class="meta"><span class="badge badge--ok">监控中</span></span>
            </label>
            <p v-if="!machines.length" class="empty">暂无机器，请先在「设置」配置 frps 并扫描</p>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <h3>选择清理项</h3>
          <span class="grow" />
          <button class="btn btn--ghost btn--sm" @click="loadItems">刷新</button>
        </div>
        <div class="card-body">
          <div class="chip-grid">
            <label v-for="it in items" :key="it.id" class="chip" :class="{ on: cleanupItems.includes(it.id) }">
              <input type="checkbox" :value="it.id" v-model="cleanupItems" />
              <span class="t">
                {{ it.name }}
                <span class="risk-tag" :class="'risk-' + it.risk">{{ riskText[it.risk] }}</span>
              </span>
              <span class="d">{{ it.desc }}</span>
            </label>
          </div>
        </div>
      </div>

      <div class="action-bar">
        <span class="hint">预览仅支持单选机器；执行前建议先预览</span>
        <button v-if="cleanupMachines.length === 1" class="btn btn--ghost" @click="preview">预览（dry-run）</button>
        <button class="btn btn--danger-solid" @click="runCleanup" :disabled="!cleanupMachines.length || !cleanupItems.length">执行清理</button>
      </div>

      <div v-if="previewResults" class="card" style="margin-top: 16px">
        <div class="card-head"><h3>预览结果（未执行任何清理）</h3></div>
        <div class="card-body">
          <div v-for="(r, i) in previewResults" :key="i" class="preview-item">
            <div class="preview-head">
              <b>{{ r.itemName }}</b>
              <span class="badge" :class="statusBadge(r.status)">{{ r.status }}</span>
            </div>
            <pre class="code-block">{{ r.output }}</pre>
          </div>
        </div>
      </div>
      <p v-if="msg" :class="msgOk ? 'msg msg--ok' : 'msg msg--err'">{{ msg }}</p>
    </div>

    <!-- 一键体检 -->
    <div v-if="tab === 'health'">
      <div class="card">
        <div class="card-head"><h3>选择机器</h3></div>
        <div class="card-body">
          <div class="choice-list">
            <label v-for="m in machines" :key="m.id" class="choice-item" :class="{ on: healthMachine === m.id }">
              <input type="radio" name="healthm" :value="m.id" v-model="healthMachine" :disabled="!m.hasCredentials" />
              <span class="truncate">{{ m.name }}</span>
              <span v-if="!m.hasCredentials" class="meta">未配置凭据</span>
            </label>
          </div>
          <div class="action-bar" style="margin-top: 16px">
            <button class="btn" @click="runHealth" :disabled="!healthMachine">开始体检</button>
          </div>
        </div>
      </div>

      <div v-if="report" class="card" style="margin-top: 16px" :class="overallBorder(report.overall)">
        <div class="card-head">
          <h3>体检报告 — {{ report.machine }}</h3>
          <span class="badge badge--accent">评分 {{ report.score }}/100</span>
          <span class="badge" :class="overallBadge(report.overall)">{{ overallText[report.overall] }}</span>
        </div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th style="width: 36px"></th><th>类别</th><th>检查项</th><th>详情</th></tr></thead>
            <tbody>
              <tr v-for="(it, i) in report.items" :key="i">
                <td class="num">{{ mark[it.status] }}</td>
                <td class="muted-cell">{{ it.category }}</td>
                <td><b>{{ it.title }}</b></td>
                <td class="muted-cell truncate" :title="it.detail">{{ it.detail }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="action-bar" style="padding: 14px 18px; margin: 0">
          <span v-if="diagnosing" class="hint">分析中（约需 10-30 秒）...</span>
          <span v-else class="hint">AI 仅输出文字建议，不会执行任何内容</span>
          <button class="btn btn--ghost" @click="runDiagnose" :disabled="diagnosing">🤖 AI 诊断分析</button>
        </div>
        <div v-if="diagnosis" class="card-body" style="border-top: 1px solid var(--border)">
          <div class="card-head" style="padding: 0 0 10px">
            <h3>AI 诊断结果</h3>
            <span v-if="diagnosis.flagged" class="badge badge--danger">⚠ 含疑似命令内容（仅供参考，未执行）</span>
            <span v-else class="badge badge--ok">纯分析内容</span>
          </div>
          <pre class="code-block diag-text">{{ diagnosis.text }}</pre>
          <p v-if="diagnosis.err" class="msg msg--err">调用失败：{{ diagnosis.err }}</p>
        </div>
      </div>

      <div v-if="history.length" class="card">
        <div class="card-head"><h3>历史体检报告</h3></div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th>时间</th><th>机器</th><th class="num">评分</th><th>结论</th></tr></thead>
            <tbody>
              <tr v-for="h in history" :key="h.id">
                <td class="muted-cell">{{ new Date(h.ts).toLocaleString() }}</td>
                <td class="truncate" :title="h.machine">{{ h.machine }}</td>
                <td class="num">{{ h.score }}</td>
                <td><span class="badge" :class="overallBadge(h.overall)">{{ overallText[h.overall] }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- 执行记录 -->
    <div v-if="tab === 'tasks'">
      <div v-for="t in tasks" :key="t.id" class="card">
        <div class="card-head">
          <h3>任务 #{{ t.id }}</h3>
          <span class="badge" :class="statusBadge(t.status)">{{ t.status }}</span>
          <span class="grow" />
          <span class="dim small">{{ t.createdAt }} · {{ t.operator }} · {{ t.type }}</span>
        </div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th style="width: 36px"></th><th>机器</th><th>项目</th><th class="num">耗时</th><th>输出</th></tr></thead>
            <tbody>
              <tr v-for="(r, i) in t.results" :key="i">
                <td class="num">{{ mark[r.status] }}</td>
                <td class="truncate" :title="r.machine">{{ r.machine }}</td>
                <td class="truncate" :title="r.itemName">{{ r.itemName }}</td>
                <td class="num muted-cell">{{ r.duration }}</td>
                <td class="mono-cell" :title="r.output">{{ r.output }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <p v-if="!tasks.length" class="empty">暂无执行记录</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { get, post } from '../api.js'

const tab = ref('cleanup')
const machines = ref([])
const items = ref([])
const cleanupMachines = ref([])
const cleanupItems = ref([])
const previewResults = ref(null)
const healthMachine = ref(null)
const report = ref(null)
const diagnosis = ref(null)
const diagnosing = ref(false)
const history = ref([])
const tasks = ref([])
const msg = ref('')
const msgOk = ref(true)
let timer = null

const riskText = { low: '低危', mid: '中危', high: '高危' }
const overallText = { pass: '健康', warn: '需关注', fail: '异常' }
const mark = { pass: '✅', warn: '⚠️', fail: '❌', ok: '✅', failed: '❌', skipped: '⏭️' }

onMounted(async () => {
  await loadMachines()
  await loadItems()
  await loadTasks()
  timer = setInterval(() => { if (tab.value === 'tasks') loadTasks() }, 3000)
})
onUnmounted(() => clearInterval(timer))

async function loadMachines() {
  const r = await get('/api/machines')
  machines.value = r.machines
}
async function loadItems() {
  const r = await get('/api/actions/cleanup-items')
  items.value = r.items
}
async function loadTasks() {
  const r = await get('/api/actions/tasks?limit=5')
  tasks.value = r.tasks
}
async function loadHistory() {
  const r = await get('/api/actions/health/reports?limit=10')
  history.value = r.reports
}

async function preview() {
  previewResults.value = null
  const r = await post('/api/actions/cleanup/preview', {
    machineIds: cleanupMachines.value, itemIds: cleanupItems.value,
  })
  previewResults.value = r.results
}

async function runCleanup() {
  if (!confirm(`确认对 ${cleanupMachines.value.length} 台机器执行 ${cleanupItems.value.length} 项清理？执行前建议先预览。`)) return
  msg.value = ''
  try {
    await post('/api/actions/cleanup', { machineIds: cleanupMachines.value, itemIds: cleanupItems.value })
    msg.value = '清理任务已创建，请在「执行记录」查看结果'
    msgOk.value = true
    tab.value = 'tasks'
    loadTasks()
  } catch (e) { msg.value = e.message; msgOk.value = false }
}

async function runHealth() {
  report.value = null
  diagnosis.value = null
  const r = await post(`/api/actions/health/${healthMachine.value}`)
  report.value = r.report
  loadHistory()
}

async function runDiagnose() {
  diagnosing.value = true
  diagnosis.value = null
  try {
    const r = await post(`/api/ai/diagnose/${healthMachine.value}`)
    diagnosis.value = r.diagnosis
  } catch (e) {
    diagnosis.value = { text: '', flagged: false, err: e.message }
  } finally {
    diagnosing.value = false
  }
}

function statusBadge(s) {
  return { pass: 'badge--ok', ok: 'badge--ok', running: 'badge--accent', warn: 'badge--warn', failed: 'badge--danger', fail: 'badge--danger', skipped: '' }[s] || ''
}
function overallBadge(o) {
  return { pass: 'badge--ok', warn: 'badge--warn', fail: 'badge--danger' }[o] || ''
}
function overallBorder(o) {
  return { pass: 'card--ok', warn: 'card--warn', fail: 'card--danger' }[o] || ''
}
</script>

<style scoped>
.truncate { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.preview-item { margin-bottom: var(--sp-4); }
.preview-item:last-child { margin-bottom: 0; }
.preview-head { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; font-size: 13.5px; }
.diag-text { max-height: 420px; overflow-y: auto; margin-top: 10px; }
.card--ok { border-color: #14553f; }
.card--warn { border-color: #4d3d14; }
.card--danger { border-color: #5b1f1f; }
</style>
