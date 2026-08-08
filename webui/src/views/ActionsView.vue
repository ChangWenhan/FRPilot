<template>
  <div>
    <h2>操作中心</h2>
    <div class="tabs">
      <button :class="{ on: tab === 'cleanup' }" @click="tab = 'cleanup'">一键清理</button>
      <button :class="{ on: tab === 'health' }" @click="tab = 'health'">一键体检</button>
      <button :class="{ on: tab === 'tasks' }" @click="tab = 'tasks'">执行记录</button>
    </div>

    <!-- 一键清理 -->
    <div v-if="tab === 'cleanup'">
      <section>
        <h3>选择机器</h3>
        <div class="chips">
          <label v-for="m in machines" :key="m.id" class="chip">
            <input type="checkbox" :value="m.id" v-model="cleanupMachines" :disabled="!m.hasCredentials" />
            {{ m.name }} <span v-if="!m.hasCredentials" class="dim">(未配置凭据)</span>
          </label>
        </div>
      </section>
      <section>
        <h3>选择清理项 <button class="small ghost" @click="loadItems">刷新</button></h3>
        <div class="chips">
          <label v-for="it in items" :key="it.id" class="chip" :class="'risk-' + it.risk">
            <input type="checkbox" :value="it.id" v-model="cleanupItems" />
            {{ it.name }}
            <span class="risk-tag">{{ riskText[it.risk] }}</span>
            <div class="dim small">{{ it.desc }}</div>
          </label>
        </div>
      </section>
      <div class="actions">
        <button v-if="cleanupMachines.length === 1" class="ghost" @click="preview">预览（dry-run）</button>
        <button class="danger" @click="runCleanup" :disabled="!cleanupMachines.length || !cleanupItems.length">执行清理</button>
        <span class="hint">预览仅支持单选机器；执行前建议先预览</span>
      </div>
      <div v-if="previewResults" class="preview">
        <h3>预览结果（未执行任何清理）</h3>
        <div v-for="(r, i) in previewResults" :key="i" class="preview-item">
          <b>{{ r.itemName }}</b> <span class="badge" :class="r.status">{{ r.status }}</span>
          <pre>{{ r.output }}</pre>
        </div>
      </div>
      <div v-if="msg" :class="msgOk ? 'ok-msg' : 'err-msg'">{{ msg }}</div>
    </div>

    <!-- 一键体检 -->
    <div v-if="tab === 'health'">
      <section>
        <h3>选择机器</h3>
        <div class="chips">
          <label v-for="m in machines" :key="m.id" class="chip">
            <input type="radio" name="healthm" :value="m.id" v-model="healthMachine" :disabled="!m.hasCredentials" />
            {{ m.name }} <span v-if="!m.hasCredentials" class="dim">(未配置凭据)</span>
          </label>
        </div>
        <div class="actions"><button @click="runHealth" :disabled="!healthMachine">开始体检</button></div>
      </section>

      <div v-if="report" class="report" :class="report.overall">
        <h3>体检报告 — {{ report.machine }}
          <span class="score">评分 {{ report.score }}/100</span>
          <span class="badge" :class="report.overall">{{ overallText[report.overall] }}</span>
        </h3>
        <div class="table-wrap"><table>
          <thead><tr><th></th><th>类别</th><th>检查项</th><th>详情</th></tr></thead>
          <tbody>
            <tr v-for="(it, i) in report.items" :key="i">
              <td>{{ mark[it.status] }}</td>
              <td>{{ it.category }}</td>
              <td><b>{{ it.title }}</b></td>
              <td class="dim">{{ it.detail }}</td>
            </tr>
          </tbody>
        </table></div>
        <div class="ai-row">
          <button class="ghost" @click="runDiagnose" :disabled="diagnosing">🤖 AI 诊断分析</button>
          <span v-if="diagnosing" class="dim">分析中（约需 10-30 秒）...</span>
        </div>
        <div v-if="diagnosis" class="diagnosis">
          <h4>AI 诊断结果
            <span v-if="diagnosis.flagged" class="badge danger">⚠ 含疑似命令内容（仅供参考，未执行）</span>
            <span v-else class="badge ok">纯分析内容</span>
          </h4>
          <p class="dim small">AI 输出仅为分析建议，系统不会执行其中任何内容。</p>
          <pre class="diag-text">{{ diagnosis.text }}</pre>
          <p v-if="diagnosis.err" class="err-msg">调用失败：{{ diagnosis.err }}</p>
        </div>
      </div>

      <section v-if="history.length">
        <h3>历史体检报告</h3>
        <div class="table-wrap"><table>
          <thead><tr><th>时间</th><th>机器</th><th>评分</th><th>结论</th></tr></thead>
          <tbody>
            <tr v-for="h in history" :key="h.id">
              <td>{{ new Date(h.ts).toLocaleString() }}</td>
              <td>{{ h.machine }}</td>
              <td>{{ h.score }}</td>
              <td><span class="badge" :class="h.overall">{{ overallText[h.overall] }}</span></td>
            </tr>
          </tbody>
        </table></div>
      </section>
    </div>

    <!-- 执行记录 -->
    <div v-if="tab === 'tasks'">
      <section v-for="t in tasks" :key="t.id">
        <h3>任务 #{{ t.id }} <span class="badge" :class="t.status">{{ t.status }}</span>
          <span class="dim">{{ t.createdAt }} · {{ t.operator }} · {{ t.type }}</span></h3>
        <div class="table-wrap"><table>
          <thead><tr><th></th><th>机器</th><th>项目</th><th>耗时</th><th>输出</th></tr></thead>
          <tbody>
            <tr v-for="(r, i) in t.results" :key="i">
              <td>{{ mark[r.status] }}</td>
              <td>{{ r.machine }}</td>
              <td>{{ r.itemName }}</td>
              <td>{{ r.duration }}</td>
              <td><pre class="inline-pre">{{ r.output }}</pre></td>
            </tr>
          </tbody>
        </table></div>
      </section>
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
</script>

<style scoped>
.tabs { display: flex; gap: 8px; margin-bottom: 16px; }
.tabs button { background: #334155; color: #cbd5e1; border: none; border-radius: 6px; padding: 8px 18px; cursor: pointer; font-size: 14px; }
.tabs button.on { background: #0ea5e9; color: #fff; }
section { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 16px; margin-bottom: 14px; }
h3 { margin: 0 0 12px; font-size: 15px; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { background: #0f172a; border: 1px solid #334155; border-radius: 8px; padding: 8px 12px; font-size: 13px; display: flex; flex-direction: column; gap: 2px; cursor: pointer; }
.risk-tag { font-size: 11px; color: #fbbf24; }
.chip.risk-high .risk-tag { color: #f87171; }
.chip.risk-low .risk-tag { color: #4ade80; }
.actions { display: flex; gap: 10px; align-items: center; margin-top: 14px; }
button { background: #0ea5e9; color: #fff; border: none; border-radius: 6px; padding: 9px 18px; cursor: pointer; font-size: 14px; }
button:disabled { opacity: 0.4; cursor: not-allowed; }
button.ghost { background: #334155; color: #cbd5e1; }
button.danger { background: #ef4444; }
button.small { padding: 4px 10px; font-size: 12px; }
.hint { color: #64748b; font-size: 12px; }
.preview { background: #0f172a; border: 1px solid #334155; border-radius: 10px; padding: 14px; margin-top: 14px; }
.preview-item { margin-bottom: 10px; }
pre { background: #0b1220; border-radius: 6px; padding: 8px; font-size: 12px; overflow-x: auto; color: #94a3b8; white-space: pre-wrap; }
.inline-pre { margin: 2px 0; max-height: 80px; overflow-y: auto; }
.ok-msg { color: #4ade80; margin-top: 10px; }
.err-msg { color: #f87171; margin-top: 10px; }
.report.pass { border-color: #14532d; }
.report.warn { border-color: #78350f; }
.report.fail { border-color: #7f1d1d; }
.score { color: #94a3b8; font-size: 13px; font-weight: 400; margin-left: 8px; }
table { width: 100%; border-collapse: collapse; margin-top: 8px; }
th, td { text-align: left; padding: 7px 8px; border-bottom: 1px solid #1e293b; font-size: 13px; }
th { color: #64748b; font-weight: 500; }
.dim { color: #64748b; }
.small { font-size: 12px; }
.empty { color: #64748b; }
.badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; background: #334155; color: #94a3b8; }
.badge.pass { background: #14532d; color: #4ade80; }
.badge.warn { background: #78350f; color: #fbbf24; }
.badge.fail { background: #7f1d1d; color: #f87171; }
.badge.ok, .badge.running { background: #1e3a8a; color: #93c5fd; }
.ai-row { margin-top: 12px; }
.diagnosis { background: #0f172a; border: 1px solid #334155; border-radius: 8px; padding: 12px; margin-top: 10px; }
.diagnosis h4 { margin: 0 0 8px; font-size: 14px; }
.diag-text { white-space: pre-wrap; color: #e2e8f0; font-size: 13px; line-height: 1.7; max-height: 400px; overflow-y: auto; }
</style>
