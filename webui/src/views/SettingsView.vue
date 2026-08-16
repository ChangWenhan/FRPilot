<template>
  <div>
    <div class="page-head">
      <h2>设置</h2>
      <span class="sub">保存后立即生效，无需重启</span>
    </div>
    <p v-if="!settings" class="empty">加载中...</p>
    <div v-if="settings">
      <div class="card">
        <div class="card-head"><h3>frps 连接信息</h3><span class="dim">热修改：保存后立即生效</span></div>
        <div class="card-body">
          <div class="field-grid">
            <div class="field">
              <label>Dashboard 地址</label>
              <input class="input" v-model="form.frps.dashboardUrl" placeholder="http://127.0.0.1:8000" />
            </div>
            <div class="field">
              <label>Dashboard 用户</label>
              <input class="input" v-model="form.frps.dashboardUser" autocomplete="off" />
            </div>
            <div class="field">
              <label>Dashboard 密码</label>
              <input class="input" v-model="form.frps.dashboardPass" type="password" autocomplete="new-password" placeholder="留空保持已保存密码" />
              <span v-if="frps.dashboardPassSet" class="secret-note">当前已加密保存，仅显示状态，不回显原文</span>
            </div>
            <div class="field">
              <label>frps SSH 主机</label>
              <input class="input" v-model="form.frps.sshHost" placeholder="如 127.0.0.1 或公网 IP" />
            </div>
            <div class="field">
              <label>frps SSH 端口</label>
              <input class="input" v-model.number="form.frps.sshPort" type="number" min="1" max="65535" />
            </div>
            <div class="field">
              <label>frps SSH 用户</label>
              <input class="input" v-model="form.frps.sshUser" autocomplete="off" />
            </div>
            <div class="field">
              <label>frps SSH 密码</label>
              <input class="input" v-model="form.frps.sshPass" type="password" autocomplete="new-password" placeholder="留空保持已保存密码" />
              <span v-if="frps.sshPassSet" class="secret-note">当前已加密保存，仅显示状态，不回显原文</span>
            </div>
            <div class="field field--wide">
              <label>frps 配置文件路径</label>
              <input class="input mono" v-model="form.frps.configPath" placeholder="/root/frp_0.57.0_linux_amd64/frps.ini" />
            </div>
          </div>

          <div class="token-line">
            <template v-if="frps.tokenSet">
              <span class="token-label">auth token 基线</span>
              <code>{{ frps.tokenMask }}</code>
              <span class="badge badge--warn">已设置 — 漂移检测以此为准，普通保存不会改动</span>
            </template>
            <template v-else>
              <span class="token-label">auth token 基线</span>
              <span class="unset">未设置</span>
              <span class="badge badge--warn">请自动检测或手动设置</span>
              <button class="btn btn--ghost btn--sm" @click="manualTokenMode = !manualTokenMode">手动设置</button>
            </template>
          </div>
          <div v-if="manualTokenMode" class="token-line manual-token">
            <input v-model="manualToken" placeholder="输入新的 token 基线" class="input" autocomplete="new-password" />
            <button class="btn btn--sm" @click="setManualToken">确认设置（将覆盖旧基线）</button>
            <button class="btn btn--ghost btn--sm" @click="manualTokenMode = false; manualToken = ''">取消</button>
            <p class="warn-text">警告：基线必须与 frps/frpc 实际配置的 token 一致，设置错误会导致全部连接失败。</p>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h3>账户策略</h3></div>
        <div class="card-body">
          <div class="field" style="max-width: 360px">
            <label>注册模式</label>
            <select class="select" v-model="form.registration">
              <option value="open">开放注册（新用户为普通用户）</option>
              <option value="approval">需管理员审批</option>
              <option value="closed">关闭注册（仅管理员建号）</option>
            </select>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h3>体检阈值</h3></div>
        <div class="card-body">
          <div class="field-grid">
            <div class="field"><label>CPU 警告 %</label><input class="input" v-model.number="form.health.cpuWarn" type="number" min="0" max="100" /></div>
            <div class="field"><label>CPU 异常 %</label><input class="input" v-model.number="form.health.cpuFail" type="number" min="0" max="100" /></div>
            <div class="field"><label>内存警告 %</label><input class="input" v-model.number="form.health.memWarn" type="number" min="0" max="100" /></div>
            <div class="field"><label>内存异常 %</label><input class="input" v-model.number="form.health.memFail" type="number" min="0" max="100" /></div>
            <div class="field"><label>磁盘警告 %</label><input class="input" v-model.number="form.health.diskWarn" type="number" min="0" max="100" /></div>
            <div class="field"><label>磁盘异常 %</label><input class="input" v-model.number="form.health.diskFail" type="number" min="0" max="100" /></div>
            <div class="field"><label>GPU 温度警告 °C</label><input class="input" v-model.number="form.health.gpuTempWarn" type="number" min="0" /></div>
            <div class="field"><label>GPU 温度异常 °C</label><input class="input" v-model.number="form.health.gpuTempFail" type="number" min="0" /></div>
            <div class="field"><label>病毒库过期天数</label><input class="input" v-model.number="form.health.clamDbMaxDays" type="number" min="1" /></div>
            <div class="field"><label>快照过期分钟</label><input class="input" v-model.number="form.health.snapshotMaxAgeMin" type="number" min="1" /></div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <h3>自定义清理命令</h3>
          <span class="grow" />
          <button class="btn btn--ghost btn--sm" @click="addCustom">+ 添加</button>
        </div>
        <div class="card-body">
          <p class="hint">追加的清理项会出现在「操作中心 → 一键清理」中，与内置项同样需要管理员确认后执行。</p>
          <div v-for="(c, i) in form.cleanupCustom" :key="i" class="custom-row">
            <div class="field"><label>名称</label><input class="input" v-model="c.name" placeholder="如 清理测试日志" /></div>
            <div class="field"><label>描述</label><input class="input" v-model="c.desc" placeholder="简要说明" /></div>
            <div class="field"><label>命令</label><input class="input mono" v-model="c.command" placeholder="要执行的命令" /></div>
            <div class="field" style="max-width: 130px"><label>风险</label>
              <select class="select" v-model="c.risk">
                <option value="low">低危</option>
                <option value="mid">中危</option>
                <option value="high">高危</option>
              </select>
            </div>
            <div class="field custom-del"><label>&nbsp;</label>
              <button class="btn btn--ghost btn--danger btn--sm" @click="form.cleanupCustom.splice(i, 1)">删除</button>
            </div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h3>AI 诊断（OpenAI 兼容协议）</h3></div>
        <div class="card-body">
          <p class="hint">AI 仅对体检报告做诊断分析，输出文字建议，系统不会执行其内容。</p>
          <div class="field-grid">
            <div class="field field--inline field--wide">
              <input class="checkbox" type="checkbox" v-model="form.ai.enabled" />
              <label style="margin: 0; color: var(--fg)">启用 AI 诊断</label>
            </div>
            <div class="field"><label>Provider 地址</label><input class="input" v-model="form.ai.providerUrl" placeholder="https://api.deepseek.com/v1" /></div>
            <div class="field"><label>模型名称</label><input class="input" v-model="form.ai.model" placeholder="deepseek-chat" /></div>
            <div class="field"><label>超时（秒）</label><input class="input" v-model.number="form.ai.timeoutSec" type="number" min="5" /></div>
            <div class="field">
              <label>API Key</label>
              <input class="input" v-model="form.ai.apiKey" type="password" autocomplete="new-password" :placeholder="aiKeyMask || '未设置'" />
              <span v-if="aiKeyMask" class="secret-note">已加密保存，留空保持不变</span>
            </div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h3>带宽控制</h3><span class="dim">基于 frps 服务器 tc 整形，按机器（隧道端口）限速，不影响任何 frpc 配置</span></div>
        <div class="card-body">
          <div class="field" style="max-width: 360px">
            <label>模式</label>
            <select class="select" v-model="form.qos.mode">
              <option value="off">关闭（不限制）</option>
              <option value="auto">自动均衡（按活跃机器数平均分）</option>
              <option value="manual">手动限速（每台固定额度）</option>
            </select>
          </div>

          <template v-if="form.qos.mode === 'auto'">
            <div class="field-grid" style="margin-top: 14px">
              <div class="field">
                <label>出站预算 Mbps（frps 对外发送数据，填了才生效）</label>
                <input class="input" type="number" min="0" step="0.1" v-model.number="form.qos.budgetOutMbps" />
              </div>
              <div class="field">
                <label>入站预算 Mbps（留空=该方向不限制）</label>
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
                <label>整形接口（留空=自动检测）</label>
                <input class="input" v-model="form.qos.interface" placeholder="如 eth0" />
              </div>
            </div>
            <p class="hint" style="margin-top: 10px">自动模式：速率超过阈值的机器视为「正在被操作」（不管开几个连接都算一台），出站预算 ÷ 活跃机器数平均分配并严格封顶；全部空闲时自动不限速。典型：出站填 3（服务器实际带宽），入站留空。</p>
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
            <p class="hint" style="margin-top: 10px">手动模式：填写的机器按固定额度限速，留空的机器不限速。</p>
          </template>
        </div>
      </div>

      <div class="action-bar">
        <button class="btn" @click="save">保存设置</button>
        <button class="btn btn--ghost" @click="detectFrps">一键自动检测 frps 配置</button>
        <button class="btn btn--ghost" @click="testFrps">测试 frps 连接</button>
        <button class="btn btn--ghost" @click="verifyToken">校验 token 基线</button>
      </div>
      <p v-if="msg" :class="msgOk ? 'msg msg--ok' : 'msg msg--err'">{{ msg }}</p>
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
const machines = ref([])
const manualMap = reactive({})
const form = reactive({ frps: {}, registration: 'open', health: {}, cleanupCustom: [], ai: {}, qos: {} })
const aiKeyMask = ref('')

onMounted(async () => {
  const s = await get('/api/settings')
  applySettings(s)
  try {
    const r = await get('/api/machines')
    machines.value = (r.machines || []).map(m => ({ name: m.name, id: m.id }))
    machines.value.forEach(m => {
      if (!manualMap[m.name]) manualMap[m.name] = { out: 0, in: 0 }
    })
  } catch {}
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
  Object.assign(form.qos, {
    mode: s.qos?.mode || 'off',
    budgetOutMbps: s.qos?.budgetOutMbps || 0,
    budgetInMbps: s.qos?.budgetInMbps || 0,
    activeKBps: s.qos?.activeKBps || 1,
    hysteresisSec: s.qos?.hysteresisSec || 45,
    interface: s.qos?.interface || '',
  })
  const manual = s.qos?.manual || []
  for (const it of manual) {
    if (!manualMap[it.name]) manualMap[it.name] = { out: 0, in: 0 }
    manualMap[it.name].out = it.outMbps || 0
    manualMap[it.name].in = it.inMbps || 0
  }
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
    const qosManual = Object.entries(manualMap)
      .filter(([name, v]) => name && (v.out > 0 || v.in > 0))
      .map(([name, v]) => ({ name, outMbps: v.out || 0, inMbps: v.in || 0 }))
    await post('/api/settings', {
      registration: form.registration,
      frps: frpsPayload,
      health: form.health,
      cleanupCustom: custom,
      ai: aiPayload,
      qos: { ...form.qos, manual: qosManual },
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
.token-line { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: var(--sp-4); font-size: 13px; color: var(--muted); }
.token-label { font-weight: 550; }
.manual-token { flex-direction: column; align-items: stretch; max-width: 520px; }
.manual-token .btn { align-self: flex-start; }
.warn-text { color: var(--warn); font-size: 12.5px; margin: 6px 0 0; }
.unset { color: var(--danger); }
.custom-row { display: grid; grid-template-columns: 1.2fr 1.5fr 2.5fr 110px 76px; gap: 12px; align-items: end; margin-bottom: var(--sp-3); }
.custom-del .btn { width: 100%; }
.manual-grid { display: grid; grid-template-columns: 1fr 160px 160px; gap: 12px; align-items: end; }
.manual-name { font-size: 13.5px; font-weight: 550; padding-bottom: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 900px) {
  .custom-row { grid-template-columns: 1fr 1fr; }
  .custom-row .field:nth-child(3) { grid-column: 1 / -1; }
  .manual-grid { grid-template-columns: 1fr 1fr; }
  .manual-name { grid-column: 1 / -1; padding-bottom: 0; }
}
@media (max-width: 560px) { .custom-row { grid-template-columns: 1fr; } }
</style>
