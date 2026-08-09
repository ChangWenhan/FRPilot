<template>
  <div>
    <h2>设置</h2>
    <p v-if="!settings" class="loading">加载中...</p>
    <div v-if="settings">
      <section>
        <h3>frps 连接信息（热修改：保存后立即生效，无需重启）</h3>
        <div class="grid">
          <label>Dashboard 地址<input class="control-input" v-model="form.frps.dashboardUrl" placeholder="http://127.0.0.1:8000" /></label>
          <label>Dashboard 用户<input class="control-input" v-model="form.frps.dashboardUser" /></label>
          <label>Dashboard 密码
            <input class="control-input" v-model="form.frps.dashboardPass" type="password" autocomplete="new-password" placeholder="留空保持已保存密码" />
            <span v-if="frps.dashboardPassSet" class="secret-note">当前已加密保存，仅显示状态，不回显原文</span>
          </label>
          <label>frps SSH 主机<input class="control-input" v-model="form.frps.sshHost" /></label>
          <label>frps SSH 端口<input class="control-input" v-model.number="form.frps.sshPort" type="number" /></label>
          <label>frps SSH 用户<input class="control-input" v-model="form.frps.sshUser" /></label>
          <label>frps SSH 密码
            <input class="control-input" v-model="form.frps.sshPass" type="password" autocomplete="new-password" placeholder="留空保持已保存密码" />
            <span v-if="frps.sshPassSet" class="secret-note">当前已加密保存，仅显示状态，不回显原文</span>
          </label>
          <label>frps 配置文件路径<input class="control-input" v-model="form.frps.configPath" placeholder="/root/frp_0.57.0_linux_amd64/frps.ini" /></label>
        </div>
        <div class="token-line">
          <template v-if="frps.tokenSet">
            auth token 基线：<code>{{ frps.tokenMask }}</code>
            <span class="badge warn">已设置 — 漂移检测以此为准，普通保存不会改动</span>
          </template>
          <template v-else>
            auth token 基线：<span class="unset">未设置</span>
            <span class="badge warn">请自动检测或手动设置</span>
            <button class="control control--sm control--ghost" @click="manualTokenMode = !manualTokenMode">手动设置</button>
          </template>
        </div>
        <div v-if="manualTokenMode" class="token-line">
          <input v-model="manualToken" placeholder="输入新的 token 基线" class="control-input token-input" autocomplete="new-password" />
          <button class="control control--sm control--success" @click="setManualToken">确认设置（将覆盖旧基线）</button>
          <button class="control control--sm control--ghost" @click="manualTokenMode = false; manualToken = ''">取消</button>
          <p class="warn-text">警告：基线必须与 frps/frpc 实际配置的 token 一致，设置错误会导致全部连接失败。</p>
        </div>
      </section>

      <section>
        <h3>账户策略</h3>
          <label>注册模式
          <select class="control-select" v-model="form.registration">
            <option value="open">开放注册（新用户为普通用户）</option>
            <option value="approval">需管理员审批</option>
            <option value="closed">关闭注册（仅管理员建号）</option>
          </select>
        </label>
      </section>

      <section>
        <h3>体检阈值</h3>
        <div class="grid">
          <label>CPU 警告 %<input class="control-input" v-model.number="form.health.cpuWarn" type="number" /></label>
          <label>CPU 异常 %<input class="control-input" v-model.number="form.health.cpuFail" type="number" /></label>
          <label>内存警告 %<input class="control-input" v-model.number="form.health.memWarn" type="number" /></label>
          <label>内存异常 %<input class="control-input" v-model.number="form.health.memFail" type="number" /></label>
          <label>磁盘警告 %<input class="control-input" v-model.number="form.health.diskWarn" type="number" /></label>
          <label>磁盘异常 %<input class="control-input" v-model.number="form.health.diskFail" type="number" /></label>
          <label>GPU 温度警告 °C<input class="control-input" v-model.number="form.health.gpuTempWarn" type="number" /></label>
          <label>GPU 温度异常 °C<input class="control-input" v-model.number="form.health.gpuTempFail" type="number" /></label>
          <label>病毒库过期天数<input class="control-input" v-model.number="form.health.clamDbMaxDays" type="number" /></label>
          <label>快照过期分钟<input class="control-input" v-model.number="form.health.snapshotMaxAgeMin" type="number" /></label>
        </div>
      </section>

      <section>
        <h3>自定义清理命令 <button class="control control--sm control--ghost" @click="addCustom">+ 添加</button></h3>
        <p class="dim small">追加的清理项会出现在「操作中心 → 一键清理」中，与内置项同样需要管理员确认后执行。</p>
        <div v-for="(c, i) in form.cleanupCustom" :key="i" class="custom-row">
          <input v-model="c.name" placeholder="名称" class="control-input short" />
          <input v-model="c.desc" placeholder="描述" class="control-input short" />
          <input v-model="c.command" placeholder="要执行的命令" class="control-input long mono" />
          <select v-model="c.risk" class="control-select short">
            <option value="low">低危</option>
            <option value="mid">中危</option>
            <option value="high">高危</option>
          </select>
          <button class="control control--sm control--danger" @click="form.cleanupCustom.splice(i, 1)">删除</button>
        </div>
      </section>

      <section>
        <h3>AI 诊断（OpenAI 兼容协议）</h3>
        <p class="dim small">AI 仅对体检报告做诊断分析，输出文字建议，系统不会执行其内容。</p>
        <div class="grid">
          <label class="chk"><input type="checkbox" v-model="form.ai.enabled" /> 启用 AI 诊断</label>
          <label>Provider 地址<input class="control-input" v-model="form.ai.providerUrl" placeholder="https://api.deepseek.com/v1" /></label>
          <label>模型名称<input class="control-input" v-model="form.ai.model" placeholder="deepseek-chat" /></label>
          <label>超时（秒）<input class="control-input" v-model.number="form.ai.timeoutSec" type="number" /></label>
          <label>API Key<input class="control-input" v-model="form.ai.apiKey" type="password" autocomplete="new-password" :placeholder="aiKeyMask || '未设置'" /><span v-if="aiKeyMask" class="secret-note">已加密保存，留空保持不变</span></label>
        </div>
      </section>

      <div class="actions">
        <button class="control" @click="save">保存设置</button>
        <button class="control control--ghost" @click="detectFrps">一键自动检测 frps 配置</button>
        <button class="control control--ghost" @click="testFrps">测试 frps 连接</button>
        <button class="control control--ghost" @click="verifyToken">校验 token 基线</button>
      </div>
      <p v-if="msg" :class="msgOk ? 'ok' : 'error'">{{ msg }}</p>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { get, post } from '../api.js'

const settings = ref(null)
const frps = ref({})
const msg = ref('')
const msgOk = ref(true)
const manualTokenMode = ref(false)
const manualToken = ref('')
const form = reactive({ frps: {}, registration: 'open', health: {}, cleanupCustom: [], ai: {} })
const aiKeyMask = ref('')

onMounted(async () => {
  const s = await get('/api/settings')
  applySettings(s)
})

function applySettings(s) {
  settings.value = s
  frps.value = s.frps
  form.registration = s.registration
  Object.assign(form.health, s.health || {})
  form.cleanupCustom = (s.cleanupCustom || []).map(x => ({ ...x }))
  Object.assign(form.ai, {
    enabled: !!s.ai?.enabled,
    providerUrl: s.ai?.providerUrl || '',
    model: s.ai?.model || '',
    timeoutSec: s.ai?.timeoutSec || 60,
    apiKey: '',
  })
  aiKeyMask.value = s.ai?.apiKeyMask || ''
  Object.assign(form.frps, {
    dashboardUrl: s.frps.dashboardUrl || '',
    dashboardUser: s.frps.dashboardUser || '',
    dashboardPass: '',
    sshHost: s.frps.sshHost || '',
    sshPort: s.frps.sshPort || 22,
    sshUser: s.frps.sshUser || '',
    sshPass: '',
    configPath: s.frps.configPath || '',
  })
}

function addCustom() {
  form.cleanupCustom.push({ name: '', desc: '', command: '', risk: 'mid' })
}

function show(m, ok = true) { msg.value = m; msgOk.value = ok }

async function save() {
  try {
    const custom = form.cleanupCustom.filter(c => c.name.trim() && c.command.trim())
    const frpsPayload = { ...form.frps }
    for (const key of ['dashboardPass', 'sshPass', 'token']) {
      if (!String(frpsPayload[key] || '').trim()) delete frpsPayload[key]
    }
    const aiPayload = { ...form.ai }
    if (!String(aiPayload.apiKey || '').trim()) delete aiPayload.apiKey
    await post('/api/settings', {
      registration: form.registration,
      frps: frpsPayload,
      health: form.health,
      cleanupCustom: custom,
      ai: aiPayload,
    })
    show('设置已保存（热修改生效，无需重启）')
  } catch (e) { show(e.message, false) }
}

async function testFrps() {
  try {
    const r = await post('/api/settings/test-frps')
    show(`连接成功：frps ${r.version}，在线客户端 ${r.clients}，当前连接 ${r.curConns}`)
  } catch (e) { show(e.message, false) }
}

async function verifyToken() {
  try {
    const r = await post('/api/settings/verify-token')
    show('token 校验通过：当前 frps token 与基线一致')
  } catch (e) { show(e.message, false) }
}

async function detectFrps() {
  if (!confirm('将从 frps 服务器自动定位配置文件，读取 bindPort/dashboard/token 等全部连接信息并写入设置。继续？')) return
  try {
    const r = await post('/api/settings/detect-frps')
    show(`自动检测完成：配置 ${r.configPath}，dashboard 端口 ${r.dashboardPort}，token 基线已安全保存`)
    await reload()
  } catch (e) { show(e.message, false) }
}

async function setManualToken() {
  const t = manualToken.value.trim()
  if (!t) { show('token 不能为空', false); return }
  if (!confirm(`将 token 基线设为 "${t}"？设置错误会导致全部 frpc 连接校验失败。`)) return
  try {
    await post('/api/settings', { registration: form.registration, frps: { token: t } })
    show('token 基线已设置')
    manualTokenMode.value = false
    manualToken.value = ''
    await reload()
  } catch (e) { show(e.message, false) }
}

async function reload() {
  const s = await get('/api/settings')
  applySettings(s)
}
</script>

<style scoped>
section { background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 18px; margin-bottom: 16px; }
h3 { margin: 0 0 14px; color: #e2e8f0; font-size: 15px; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 12px; }
@media (max-width: 768px) {
  .grid { grid-template-columns: 1fr; }
  .custom-row { flex-direction: column; align-items: stretch; }
  .custom-row input, .custom-row select { width: 100% !important; }
  .token-input { width: 100%; }
  .actions { flex-wrap: wrap; }
}
label { display: flex; flex-direction: column; gap: 6px; font-size: 13px; color: #94a3b8; }
input, select {
  background: #0f172a; border: 1px solid #334155; border-radius: 6px;
  padding: 9px 10px; color: #e2e8f0; font-size: 14px;
}
.token-line { margin-top: 14px; font-size: 13px; color: #94a3b8; }
.custom-row { display: flex; gap: 8px; margin-bottom: 8px; align-items: center; }
.custom-row input, .custom-row select { background: #0f172a; border: 1px solid #334155; border-radius: 6px; padding: 8px 10px; color: #e2e8f0; font-size: 13px; }
.short { width: 130px; }
.long { flex: 1; }
.mono { font-family: ui-monospace, monospace; }
.chk { flex-direction: row !important; align-items: center; }
button.small { padding: 5px 10px; font-size: 12px; border-radius: 5px; }
button.danger { background: #ef4444; }
code { background: #0f172a; padding: 2px 8px; border-radius: 4px; color: #4ade80; }
.badge.warn { background: #78350f; color: #fbbf24; padding: 2px 8px; border-radius: 4px; font-size: 12px; margin-left: 6px; }
.unset { color: #f87171; }
button.small { padding: 4px 10px; font-size: 12px; margin-left: 8px; border-radius: 5px; border: none; cursor: pointer; }
.ok { background: #0ea5e9; color: #fff; }
.ghost { background: #334155; color: #cbd5e1; }
.token-input { width: 240px; margin-right: 8px; }
.warn-text { color: #fbbf24; font-size: 12px; }
.actions { display: flex; gap: 10px; }
button { background: #0ea5e9; color: #fff; border: none; border-radius: 6px; padding: 9px 18px; cursor: pointer; font-size: 14px; }
button.ghost { background: #334155; color: #cbd5e1; }
.ok { color: #4ade80; }
.error { color: #f87171; }
.loading { color: #64748b; }
</style>
