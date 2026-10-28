<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, apiAll, session } from '../api'
import { createClientId } from '../clientId'
import { parseDeviceRows } from '../onboardingPlan'
import { UiMessage, UiMessageBox } from '../ui/feedback'
import { copyText } from '../clipboard'
import { useUnsavedGuard } from '../composables/unsavedGuard.js'
import { confirmed } from '../deleteAction'
const props = defineProps({ batchId: { type: String, default: '' } })
const emit = defineEmits(['close', 'detail'])
const products = ref([]),
  productId = ref(''),
  profiles = ref([]),
  profileId = ref(''),
  plan = ref(null),
  transport = ref('MQTT'),
  mode = ref('')
const text = ref(''),
  error = ref(''),
  busy = ref(''),
  preflight = ref(null),
  batch = ref(null),
  page = ref(1),
  secrets = ref(null),
  savedRequest = ref(null)
const requestId = ref(props.batchId || createClientId())
// 领取的密钥只在当前页面显示；复制或下载视为已保存，此前离开页面、切换菜单或刷新都先提醒。
const secretsSaved = ref(false)
const unsavedSecrets = computed(() => Boolean(secrets.value?.items?.length) && !secretsSaved.value)
useUnsavedGuard(() => unsavedSecrets.value)
async function confirmSecretsLeave() {
  if (!unsavedSecrets.value) return true
  try {
    await UiMessageBox.confirm(
      '本次领取的设备密钥只显示这一次，离开后无法再次查看，只能到设备详情中逐台重新生成凭据。请先复制或下载保存。',
      '设备密钥尚未保存',
      { confirmButtonText: '已保存，继续离开', cancelButtonText: '返回保存' }
    )
  } catch {
    return false
  }
  secrets.value = null
  return true
}
async function leave(event, ...args) {
  if (await confirmSecretsLeave()) emit(event, ...args)
}
let timer = 0,
  version = 0,
  refreshVersion = 0,
  active = true
const identityToken = session.token
const currentIdentity = () => active && session.token === identityToken
const deviceRole = computed(() => (products.value.find(p => p.id === productId.value)?.category === 'gateway' ? 'GATEWAY' : 'DIRECT'))
const defaultConnection = computed(() => ({
  mode: mode.value || plan.value?.mode || '',
  ...(plan.value?.mode === 'standard' ? { transport: transport.value } : {}),
  ...(plan.value?.mode === 'listener' && mode.value !== 'dial' ? { profileId: profileId.value } : {})
}))
const awaiting = computed(() => batch.value && ['INITIALIZING', 'PENDING', 'RUNNING', 'QUEUED'].includes(batch.value.status))
const failedIndices = computed(() => batch.value?.rows?.filter(row => row.status === 'FAILED').map(row => row.index) || [])
const pendingIndices = computed(
  () => batch.value?.rows?.filter(row => ['PENDING', 'RUNNING'].includes(row.status)).map(row => row.index) || []
)
const statusNames = {
  INITIALIZING: '准备登记',
  PARTIAL_FAILED: '部分登记失败',
  PAUSED: '等待检查权限或配置后继续',
  PENDING: '待处理',
  RUNNING: '登记中',
  SUCCEEDED: '已登记，等待真实验证',
  FAILED: '登记失败',
  COMPLETED: '登记已完成',
  PARTIAL: '部分失败',
  INTERRUPTED: '已中断',
  QUEUED: '等待处理'
}
const onboardingNames = {
  REGISTERED: '已登记',
  WAITING_CONFIGURATION: '待现场配置',
  WAITING_VERIFICATION: '待真实设备验证',
  VERIFIED: '已验证',
  ERROR: '配置或验证异常'
}
const credentialNames = {
  NONE: '无需平台密钥',
  AVAILABLE: '可领取',
  DELIVERED: '已领取',
  EXPIRED: '已过期，请到设备详情重签',
  REISSUE_REQUIRED: '请到设备详情重签'
}
function invalidate() {
  preflight.value = null
  savedRequest.value = null
}
watch([productId, profileId, mode, transport, text], invalidate)
async function chooseProduct() {
  const current = ++version
  plan.value = null
  profiles.value = []
  profileId.value = ''
  error.value = ''
  if (!productId.value) return
  try {
    const data = await api(`/api/v1/onboarding/preflight?productId=${encodeURIComponent(productId.value)}`)
    if (current !== version || !currentIdentity()) return
    plan.value = data.plan
    profiles.value = (data.profiles || []).filter(p => p.enabled && p.publicHost)
    profileId.value = profiles.value[0]?.id || ''
    mode.value = data.plan?.mode || ''
    transport.value = data.plan?.connector === 'HTTP' ? 'HTTP' : 'MQTT'
    if (!data.ready) error.value = data.plan?.reason || '设备模板暂不可用'
  } catch (cause) {
    if (current === version) error.value = cause.message
  }
}
async function chooseFile(event) {
  const file = event.target.files?.[0]
  if (!file) return
  if (file.size > 2 * 1024 * 1024) {
    error.value = '清单不能超过 2 MiB'
    return
  }
  text.value = await file.text()
}
async function check() {
  if (busy.value) return
  busy.value = 'check'
  error.value = ''
  try {
    if (!productId.value || !plan.value) throw new Error('请选择设备模板')
    if (defaultConnection.value.mode === 'listener' && !profileId.value) throw new Error('模板还没有可用公共连接，请先在模板中准备')
    const body = {
      productId: productId.value,
      connection: defaultConnection.value,
      rows: parseDeviceRows(text.value, defaultConnection.value.mode, deviceRole.value)
    }
    const snapshot = JSON.stringify(body)
    const result = await api('/api/v1/onboarding/batches/preflight', { method: 'POST', body: snapshot })
    if (
      snapshot !==
      JSON.stringify({
        productId: productId.value,
        connection: defaultConnection.value,
        rows: parseDeviceRows(text.value, defaultConnection.value.mode, deviceRole.value)
      })
    )
      return
    preflight.value = result
    savedRequest.value = body
  } catch (cause) {
    error.value = cause.message
  } finally {
    busy.value = ''
  }
}
async function submit() {
  if (busy.value || !preflight.value || !savedRequest.value) return
  busy.value = 'submit'
  error.value = ''
  try {
    batch.value = await api('/api/v1/onboarding/batches', {
      method: 'POST',
      body: JSON.stringify({ ...savedRequest.value, id: requestId.value, fingerprint: preflight.value.fingerprint })
    })
    await refresh()
  } catch (cause) {
    error.value = cause.message
    invalidate()
  } finally {
    busy.value = ''
  }
}
async function refresh() {
  const current = ++refreshVersion
  try {
    const result = await api(`/api/v1/onboarding/batches/${encodeURIComponent(requestId.value)}?limit=30&offset=${(page.value - 1) * 30}`)
    if (current !== refreshVersion || !currentIdentity()) return
    batch.value = result
    error.value = ''
  } catch (cause) {
    if (current === refreshVersion && currentIdentity()) error.value = cause.message
  }
}
async function retry(indices = failedIndices.value) {
  if (busy.value || !indices.length) return
  busy.value = 'retry'
  try {
    await api(`/api/v1/onboarding/batches/${encodeURIComponent(requestId.value)}/retry`, {
      method: 'POST',
      body: JSON.stringify({ indices, revision: batch.value.revision })
    })
    await refresh()
  } catch (cause) {
    error.value = cause.message
  } finally {
    busy.value = ''
  }
}
async function claim() {
  if (busy.value) return
  busy.value = 'credentials'
  try {
    const result = await api(`/api/v1/onboarding/batches/${encodeURIComponent(requestId.value)}/credentials`, {
      method: 'POST',
      body: '{}'
    })
    if (!currentIdentity()) return
    secretsSaved.value = false
    secrets.value = result
    await refresh()
  } catch (cause) {
    error.value = cause.message
  } finally {
    busy.value = ''
  }
}
async function clearSecrets() {
  if (!secretsSaved.value && secrets.value?.items?.length) {
    if (
      !(await confirmed('清除后无法再次查看本次领取的设备密钥，确认已复制或下载保存？', '清除设备密钥', {
        confirmButtonText: '已保存，清除',
        cancelButtonText: '返回保存'
      }))
    )
      return
  }
  secrets.value = null
}
function downloadJSON(name, value) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  URL.revokeObjectURL(url)
}
function exportConfiguration() {
  downloadJSON(
    `device-configuration-${requestId.value}-page-${page.value}.json`,
    (batch.value.rows || []).map(row => ({
      deviceId: row.deviceId,
      name: row.name,
      mode: row.mode,
      profileId: row.profileId,
      accessInfo: row.accessInfo || null,
      status: row.status,
      error: row.error
    }))
  )
}
function exportSecrets() {
  if (!currentIdentity() || !secrets.value) return
  downloadJSON(`device-credentials-${requestId.value}.json`, secrets.value.items || [])
  secrets.value = null
  UiMessage.success('已发起下载并清除页面中的密钥，请确认文件已保存')
}
async function copySecrets() {
  if (await copyText(JSON.stringify(secrets.value.items, null, 2))) {
    secretsSaved.value = true
    UiMessage.success('凭据已复制，请妥善保存')
  } else UiMessage.warning('复制失败，请手动复制')
}
watch(page, () => {
  if (batch.value) refresh()
})
onMounted(async () => {
  try {
    if (props.batchId) await refresh()
    else products.value = ((await apiAll('/api/v1/products')).items || []).filter(p => p.reusable === true && p.status === 'ENABLED')
    timer = setInterval(() => {
      if (awaiting.value && document.visibilityState !== 'hidden') refresh()
    }, 3000)
  } catch (cause) {
    error.value = cause.message
  }
})
onBeforeUnmount(() => {
  active = false
  version++
  refreshVersion++
  clearInterval(timer)
  secrets.value = null
})
</script>
<template>
  <section class="batch-onboarding">
    <header>
      <div>
        <h2>批量添加设备</h2>
        <p>复用已经验证的设备模板，逐台登记并保留结果；登记成功后仍需真实上报验证。</p>
      </div>
      <ui-button @click="leave('close')">返回设备列表</ui-button>
    </header>
    <ui-alert v-if="error" :title="error" type="error" :closable="false" />
    <template v-if="!batch">
      <ui-form label-position="top"
        ><ui-form-item label="设备模板"
          ><ui-select v-model="productId" filterable @change="chooseProduct"
            ><ui-option v-for="p in products" :key="p.id" :value="p.id" :label="p.name" /></ui-select
        ></ui-form-item>
        <ui-form-item v-if="plan?.mode === 'listener'" label="连接方式"
          ><ui-select v-model="mode"
            ><ui-option value="listener" label="设备连接平台" /><ui-option
              v-if="plan.dial"
              value="dial"
              label="平台主动连接设备" /></ui-select
        ></ui-form-item>
        <ui-form-item v-if="plan?.mode === 'listener' && mode !== 'dial'" label="公共连接"
          ><ui-select v-model="profileId"
            ><ui-option v-for="p in profiles" :key="p.id" :value="p.id" :label="`${p.publicHost}:${p.port}`" /></ui-select
        ></ui-form-item>
        <ui-form-item v-if="plan?.mode === 'standard'" label="上报通道"
          ><ui-select v-model="transport"><ui-option value="MQTT" label="MQTT" /><ui-option value="HTTP" label="HTTP" /></ui-select
        ></ui-form-item>
        <ui-form-item label="设备清单"
          ><div class="batch-input">
            <p>
              CSV 或制表符分隔，列顺序：设备编号、设备名称、位置备注{{
                ['dial', 'poll'].includes(mode) ? '、设备地址、端口、站号' : ''
              }}。最多 1000 台。
            </p>
            <input type="file" accept=".csv,.tsv,.txt" aria-label="上传设备清单" @change="chooseFile" /><ui-input
              v-model="text"
              type="textarea"
              :rows="9"
              placeholder="设备编号,设备名称,位置备注&#10;sensor_001,一层东侧烟感,一层"
            /></div></ui-form-item
      ></ui-form>
      <ui-button :loading="busy === 'check'" :disabled="!!busy || !text.trim()" @click="check">检查清单</ui-button>
      <template v-if="preflight"
        ><p>共 {{ preflight.total }} 台，{{ preflight.rows.filter(row => row.valid).length }} 台通过登记检查。</p>
        <ui-table :data="preflight.rows"
          ><ui-table-column label="行" width="60"
            ><template #default="{ row }">{{ row.index + 1 }}</template></ui-table-column
          ><ui-table-column prop="deviceId" label="设备编号" /><ui-table-column label="检查结果"
            ><template #default="{ row }">{{ row.valid ? '可登记' : row.error }}</template></ui-table-column
          ></ui-table
        ><ui-button type="primary" :disabled="!!busy || preflight.rows.some(row => !row.valid)" :loading="busy === 'submit'" @click="submit"
          >确认登记 {{ preflight.total }} 台设备</ui-button
        ></template
      >
    </template>
    <template v-else
      ><div class="batch-summary">
        <strong>{{ statusNames[batch.status] || batch.status }}</strong
        ><span>共 {{ batch.total }} 台 · 已登记 {{ batch.succeeded }} · 失败 {{ batch.failed }} · 待处理 {{ batch.pending }}</span
        ><ui-button size="small" @click="refresh">刷新</ui-button
        ><ui-button size="small" @click="exportConfiguration">下载本页现场配置</ui-button
        ><ui-button v-if="failedIndices.length && !awaiting" :loading="busy === 'retry'" @click="retry()">重试本页失败项</ui-button
        ><ui-button v-if="batch.status === 'PAUSED' && pendingIndices.length" :disabled="!!busy" @click="retry(pendingIndices)"
          >检查权限后继续本页待处理项</ui-button
        ><ui-button :disabled="!!busy || !!secrets?.items?.length" :loading="busy === 'credentials'" @click="claim"
          >领取本批设备密钥</ui-button
        >
      </div>
      <p>后台保留任务，关闭页面后可从“批量记录”继续查看。密钥限时领取，领取后只在当前页面显示，离开前请保存。</p>
      <p v-if="batch.verificationSummary">
        当前页验收：已验证 {{ batch.verificationSummary.verified }} · 待配置 {{ batch.verificationSummary.waitingConfiguration }} · 待验证
        {{ batch.verificationSummary.waitingVerification }} · 异常 {{ batch.verificationSummary.errors }}
      </p>
      <ui-alert v-if="batch.error" :title="batch.error" type="warning" :closable="false" />
      <ui-table :data="batch.rows || []"
        ><ui-table-column prop="deviceId" label="设备编号" min-width="150" show-overflow-tooltip /><ui-table-column
          prop="name"
          label="名称"
          min-width="140"
          show-overflow-tooltip
        /><ui-table-column label="登记状态" min-width="130"
          ><template #default="{ row }">{{ statusNames[row.status] || row.status }}</template></ui-table-column
        ><ui-table-column label="现场状态" min-width="130"
          ><template #default="{ row }">{{ onboardingNames[row.onboardingStatus] || '待现场配置与验证' }}</template></ui-table-column
        ><ui-table-column label="密钥" min-width="130"
          ><template #default="{ row }">{{ credentialNames[row.credentialStatus] || '—' }}</template></ui-table-column
        ><ui-table-column prop="error" label="原因" min-width="200" show-overflow-tooltip /><ui-table-column label="验证" min-width="150"
          ><template #default="{ row }"
            ><ui-button v-if="row.status === 'SUCCEEDED'" size="small" @click="leave('detail', row.deviceId)"
              >配置、验证与密钥</ui-button
            ></template
          ></ui-table-column
        ></ui-table
      >
      <ui-pagination v-if="batch.total > 30" v-model:current-page="page" :page-size="30" :total="batch.total" layout="prev,pager,next" />
      <section v-if="secrets" class="batch-secrets">
        <strong>设备密钥只在领取时显示</strong
        ><ui-button v-if="secrets.items?.length" size="small" @click="copySecrets">复制本次密钥</ui-button
        ><ui-button v-if="secrets.items?.length" size="small" @click="exportSecrets">下载本次密钥与配置</ui-button>
        <pre v-for="item in secrets.items" :key="item.index"
          >{{ item.deviceId }} · {{ item.credential.accessKey }}
{{ item.credential.secret }}</pre>
        <p v-for="item in secrets.unavailable" :key="item.index">{{ item.deviceId }}：{{ item.reason }}</p>
        <ui-button @click="clearSecrets">我已保存，清除显示</ui-button>
      </section>
    </template>
  </section>
</template>
<style scoped>
.batch-onboarding {
  max-width: 1100px;
  margin: 0 auto;
  display: grid;
  gap: var(--space-4);
}
.batch-onboarding header,
.batch-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
}
.batch-onboarding header > div {
  flex: 1;
}
.batch-onboarding h2 {
  margin: 0;
}
.batch-onboarding p {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.batch-input {
  display: grid;
  gap: var(--space-2);
  width: 100%;
}
.batch-secrets {
  padding: var(--space-4);
  border: 1px solid var(--warning-border);
  border-radius: var(--radius-md);
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
