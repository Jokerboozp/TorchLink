<script setup>
// 摄像头直播配置：与摄像头基础资料分开保存，编辑名称和位置不会影响这里的配置。
// 密码只写不读：留空保留已保存的密码，勾选“清除”才删除。
import { computed, reactive, ref, watch } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, formatTime, notifyError } from '../api'
import { liveState, testStatusText, testStatusTone } from '../liveVideo'
import StatusDot from './layout/StatusDot.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  camera: { type: Object, default: null }
})
const emit = defineEmits(['update:modelValue', 'saved'])
const visible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })

const blank = () => ({
  enabled: true,
  accessMode: 'RTSP',
  brandTemplate: 'hikvision',
  host: '',
  rtspPort: 554,
  onvifPort: 80,
  channel: 1,
  nvr: false,
  manualUrl: false,
  mainStreamUrl: '',
  subStreamUrl: '',
  mainProfileToken: '',
  subProfileToken: '',
  defaultStream: 'sub',
  transcodeMode: 'off',
  transcodeProfile: 'h264_720p',
  sourceBFrames: false,
  username: '',
  password: '',
  clearPassword: false,
  gbDeviceId: '',
  gbChannelId: ''
})
const form = reactive(blank())
const hasPassword = ref(false)
const loading = ref(false),
  saving = ref(false),
  testing = ref(false),
  querying = ref(false)
const testStream = ref('main')
const testResult = ref(null)
const lastTest = ref(null)
const profiles = ref([])
const onvifError = ref('')
let openVersion = 0

const brands = computed(() => liveState.status?.brands || [])
const brand = computed(() => brands.value.find(item => item.id === form.brandTemplate))
const structured = computed(() => form.accessMode === 'RTSP' && brand.value?.structured && !form.manualUrl)
const transcodeAvailable = computed(() => Boolean(liveState.status?.transcodeAvailable))
const transcodeProfiles = computed(() => (liveState.status?.profiles || []).filter(item => item.transcode))
const busy = computed(() => saving.value || testing.value || querying.value)
const isGB = computed(() => form.accessMode === 'GB28181')
const gbDevices = ref([])
const gbLoading = ref(false)
const gbDevice = computed(() => gbDevices.value.find(item => item.deviceId === form.gbDeviceId) || null)
const gbChannels = computed(() => gbDevice.value?.state?.channels || [])
const gbChannelLabel = row => `${row.name || row.channelId} · ${row.channelId}${row.status === 'OFF' ? '（离线）' : ''}`

// 国标设备列表按需读取：只有选择 GB28181 接入时才需要。
async function loadGBDevices() {
  if (gbLoading.value) return
  gbLoading.value = true
  try {
    gbDevices.value = (await api('/api/v1/integrations/video/gb28181/devices')).items || []
  } catch (error) {
    notifyError(error)
  } finally {
    gbLoading.value = false
  }
}
watch(isGB, value => {
  if (value && props.modelValue) loadGBDevices()
})
// 用户切换设备后默认选第一个通道；载入已保存的配置时不改动通道。
let applyingConfig = false
watch(
  () => form.gbDeviceId,
  (id, previous) => {
    if (applyingConfig || !id || id === previous || gbChannels.value.some(item => item.channelId === form.gbChannelId)) return
    form.gbChannelId = gbChannels.value[0]?.channelId || id
  },
  { flush: 'sync' }
)

watch(
  () => [props.modelValue, props.camera?.cameraId],
  async ([open]) => {
    if (!open || !props.camera?.cameraId) return
    const version = ++openVersion
    applyingConfig = true
    Object.assign(form, blank())
    applyingConfig = false
    testResult.value = null
    lastTest.value = null
    profiles.value = []
    onvifError.value = ''
    hasPassword.value = false
    loading.value = true
    try {
      const cfg = await api(`/api/v1/integrations/video/cameras/${encodeURIComponent(props.camera.cameraId)}/live`)
      if (version !== openVersion) return
      const configured = Boolean(cfg.updatedAt)
      applyingConfig = true
      Object.assign(form, blank(), Object.fromEntries(Object.entries(cfg).filter(([key]) => key in form)), {
        password: '',
        clearPassword: false
      })
      applyingConfig = false
      if (!configured) form.enabled = true
      if (!form.transcodeProfile) form.transcodeProfile = 'h264_720p'
      if (!form.rtspPort) form.rtspPort = 554
      if (!form.onvifPort) form.onvifPort = 80
      hasPassword.value = Boolean(cfg.hasPassword)
      lastTest.value = cfg.lastTest || null
      if (form.accessMode === 'GB28181') loadGBDevices()
    } catch (error) {
      if (version === openVersion) {
        notifyError(error)
        visible.value = false
      }
    } finally {
      if (version === openVersion) loading.value = false
    }
  },
  { immediate: true }
)

function payload(extra = {}) {
  const value = {
    ...form,
    host: form.host.trim(),
    username: form.username.trim(),
    mainStreamUrl: form.mainStreamUrl.trim(),
    subStreamUrl: form.subStreamUrl.trim(),
    ...extra
  }
  value.rtspPort = Number(value.rtspPort) || 0
  value.onvifPort = Number(value.onvifPort) || 0
  value.channel = Number(value.channel) || 1
  if (value.transcodeMode !== 'fixed' && value.transcodeMode !== 'auto') value.transcodeProfile = ''
  return value
}
const cameraPath = () => `/api/v1/integrations/video/cameras/${encodeURIComponent(props.camera.cameraId)}/live`

async function queryProfiles() {
  if (querying.value) return
  if (!form.host.trim()) return UiMessage.warning('请填写设备地址')
  querying.value = true
  onvifError.value = ''
  try {
    const data = await api(`${cameraPath()}/onvif-profiles`, { method: 'POST', body: JSON.stringify(payload()) })
    profiles.value = data.items || []
    onvifError.value = data.error || (profiles.value.length ? '' : '设备未返回可用的媒体配置')
    if (profiles.value.length && !profiles.value.some(item => item.token === form.mainProfileToken))
      form.mainProfileToken = profiles.value[0].token
    if (profiles.value.length > 1 && !profiles.value.some(item => item.token === form.subProfileToken))
      form.subProfileToken = profiles.value[1].token
  } catch (error) {
    notifyError(error)
  } finally {
    querying.value = false
  }
}

async function runTest() {
  if (testing.value) return
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await api(`${cameraPath()}/test`, { method: 'POST', body: JSON.stringify(payload({ stream: testStream.value })) })
  } catch (error) {
    notifyError(error)
  } finally {
    testing.value = false
  }
}

async function save() {
  if (saving.value) return
  if (form.accessMode === 'ONVIF' && !form.mainProfileToken) return UiMessage.warning('请先查询并选择主码流媒体配置')
  if (isGB.value && (!form.gbDeviceId || !form.gbChannelId)) return UiMessage.warning('请选择国标设备和通道')
  saving.value = true
  try {
    await api(cameraPath(), { method: 'PUT', body: JSON.stringify(payload()) })
    UiMessage.success('直播配置已保存')
    emit('saved')
    visible.value = false
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}

const resultTone = result => testStatusTone[result?.status] || 'danger'
function resultDetail(result) {
  const parts = []
  if (result.videoCodec) parts.push(`视频 ${result.videoCodec}${result.h264Profile ? `（${result.h264Profile}）` : ''}`)
  if (result.width) parts.push(`${result.width}×${result.height}${result.fps ? ` · ${result.fps} fps` : ''}`)
  if (result.audioCodec) parts.push(`音频 ${result.audioCodec}`)
  parts.push(result.mediaVerified ? '媒体服务已收到画面' : '未经媒体服务确认')
  return parts.join(' · ')
}
</script>

<template>
  <ui-dialog
    v-model="visible"
    :title="`直播配置 · ${camera?.cameraName || camera?.cameraId || ''}`"
    width="min(760px, 96vw)"
    :close-on-click-modal="false"
    destroy-on-close
  >
    <div v-loading="loading" class="live-config">
      <ui-alert
        type="info"
        :closable="false"
        title="直播配置与摄像头基础资料分开保存。品牌模板只用于生成常见地址，不代表所有型号都兼容；保存前请先连接测试。只支持厂商 App 或 P2P 私有云、没有 ONVIF / RTSP / GB28181 的设备无法接入。"
      />
      <ui-form :model="form" label-position="top" :disabled="busy">
        <section class="live-config__section">
          <div class="live-config__row">
            <ui-form-item label="启用该摄像头直播"><ui-switch v-model="form.enabled" /></ui-form-item>
            <ui-form-item label="接入方式"
              ><ui-radio-group v-model="form.accessMode" class="segmented-choice-group"
                ><ui-radio-button value="RTSP">RTSP</ui-radio-button><ui-radio-button value="ONVIF">ONVIF</ui-radio-button
                ><ui-radio-button value="GB28181">GB28181</ui-radio-button></ui-radio-group
              ></ui-form-item
            >
          </div>
        </section>

        <section v-if="isGB" class="live-config__section">
          <h3>国标设备与通道</h3>
          <div class="live-config__grid">
            <ui-form-item label="国标设备"
              ><ui-select v-model="form.gbDeviceId" filterable :placeholder="gbLoading ? '读取中…' : '选择已登记的设备'"
                ><ui-option
                  v-for="item in gbDevices"
                  :key="item.deviceId"
                  :value="item.deviceId"
                  :label="`${item.name || item.deviceId} · ${item.deviceId}${item.online ? '' : '（不在线）'}`" /></ui-select
            ></ui-form-item>
            <ui-form-item label="通道"
              ><ui-select v-model="form.gbChannelId" filterable allow-create placeholder="选择通道，或输入 20 位通道编号"
                ><ui-option
                  v-for="item in gbChannels"
                  :key="item.channelId"
                  :value="item.channelId"
                  :label="gbChannelLabel(item)" /></ui-select
            ></ui-form-item>
          </div>
          <p class="live-config__hint">
            设备需先在摄像头页的“国标设备”中登记并注册上线；通道列表来自设备上报的目录，未上报时可直接填写通道编号。国标接入只有一路码流，账号密码在设备注册时校验。
          </p>
        </section>

        <section v-if="!isGB" class="live-config__section">
          <h3>设备地址与账号</h3>
          <div class="live-config__grid">
            <ui-form-item v-if="form.accessMode === 'RTSP'" label="品牌模板"
              ><ui-select v-model="form.brandTemplate"
                ><ui-option v-for="item in brands" :key="item.id" :value="item.id" :label="item.name" /></ui-select
            ></ui-form-item>
            <ui-form-item v-if="form.accessMode === 'ONVIF' || structured" label="设备或 NVR 地址"
              ><ui-input v-model="form.host" placeholder="例如 192.168.1.64"
            /></ui-form-item>
            <ui-form-item v-if="structured" label="RTSP 端口"
              ><ui-input-number v-model="form.rtspPort" :min="1" :max="65535"
            /></ui-form-item>
            <ui-form-item v-if="structured" label="通道号"><ui-input-number v-model="form.channel" :min="1" :max="512" /></ui-form-item>
            <ui-form-item v-if="form.accessMode === 'ONVIF'" label="ONVIF 端口"
              ><ui-input-number v-model="form.onvifPort" :min="1" :max="65535"
            /></ui-form-item>
            <ui-form-item label="用户名"><ui-input v-model="form.username" autocomplete="off" /></ui-form-item>
            <ui-form-item label="密码"
              ><ui-input
                v-model="form.password"
                type="password"
                show-password
                autocomplete="new-password"
                :placeholder="hasPassword ? '已保存，留空则不修改' : '未设置'"
                :disabled="form.clearPassword"
            /></ui-form-item>
          </div>
          <ui-checkbox v-if="hasPassword" v-model="form.clearPassword">清除已保存的密码</ui-checkbox>
          <p v-if="form.accessMode === 'RTSP' && brand?.hint" class="live-config__hint">
            {{ brand.hint }}<template v-if="brand.structured"> 连接 NVR 时填写 NVR 地址和摄像头所在通道号。</template>
          </p>
          <ui-checkbox v-if="form.accessMode === 'RTSP' && brand?.structured" v-model="form.manualUrl">手动填写流地址</ui-checkbox>
          <div v-if="form.accessMode === 'RTSP' && !structured" class="live-config__grid">
            <ui-form-item label="主码流地址"
              ><ui-input v-model="form.mainStreamUrl" placeholder="rtsp://192.168.1.64:554/…（不含账号密码）"
            /></ui-form-item>
            <ui-form-item label="子码流地址（可选）"><ui-input v-model="form.subStreamUrl" placeholder="rtsp://…" /></ui-form-item>
          </div>
          <div v-if="form.accessMode === 'ONVIF'" class="live-config__onvif">
            <ui-button size="small" :loading="querying" @click="queryProfiles">查询媒体配置</ui-button>
            <p v-if="onvifError" class="live-config__error">{{ onvifError }}</p>
            <ui-table v-if="profiles.length" :data="profiles" size="small">
              <ui-table-column label="媒体配置" min-width="160"
                ><template #default="{ row }"
                  ><b>{{ row.name || row.token }}</b
                  ><small class="subline">{{ row.token }}</small></template
                ></ui-table-column
              >
              <ui-table-column label="编码 / 分辨率" min-width="130"
                ><template #default="{ row }"
                  >{{ row.encoding || '—' }}<small v-if="row.width" class="subline">{{ row.width }}×{{ row.height }}</small></template
                ></ui-table-column
              >
              <ui-table-column label="用途" width="170"
                ><template #default="{ row }"
                  ><ui-radio
                    :checked="form.mainProfileToken === row.token"
                    :disabled="Boolean(row.error)"
                    @update:checked="form.mainProfileToken = row.token"
                    >主码流</ui-radio
                  ><ui-radio
                    :checked="form.subProfileToken === row.token"
                    :disabled="Boolean(row.error)"
                    @update:checked="form.subProfileToken = row.token"
                    >子码流</ui-radio
                  ></template
                ></ui-table-column
              >
            </ui-table>
          </div>
        </section>

        <section class="live-config__section">
          <h3>播放与转码</h3>
          <div class="live-config__grid">
            <ui-form-item v-if="!isGB" label="默认码流"
              ><ui-radio-group v-model="form.defaultStream" class="segmented-choice-group"
                ><ui-radio-button value="sub">子码流</ui-radio-button><ui-radio-button value="main">主码流</ui-radio-button></ui-radio-group
              ></ui-form-item
            >
            <ui-form-item label="转码策略"
              ><ui-select v-model="form.transcodeMode"
                ><ui-option value="off" label="关闭：只播放兼容码流" /><ui-option
                  value="auto"
                  label="自动：按源编码和浏览器能力判断"
                  :disabled="!transcodeAvailable" /><ui-option
                  value="fixed"
                  label="指定兼容输出"
                  :disabled="!transcodeAvailable" /></ui-select
            ></ui-form-item>
            <ui-form-item v-if="form.transcodeMode !== 'off'" :label="form.transcodeMode === 'fixed' ? '输出规格' : '自动转码目标'"
              ><ui-select v-model="form.transcodeProfile"
                ><ui-option
                  v-for="item in transcodeProfiles.filter(p => form.transcodeMode === 'fixed' || p.id !== 'audio_aac')"
                  :key="item.id"
                  :value="item.id"
                  :label="item.name" /></ui-select
            ></ui-form-item>
          </div>
          <ui-checkbox v-model="form.sourceBFrames">源 H.264 含 B 帧（WebRTC 播放需转码）</ui-checkbox>
          <p class="live-config__hint">
            转协议（RTSP / GB28181 → WebRTC / HLS）不改变编码；转码会重新编码并占用媒体服务 CPU。<template v-if="!transcodeAvailable"
              >当前部署未启用转码。</template
            >播放默认静音。
          </p>
        </section>

        <section class="live-config__section">
          <h3>连接测试</h3>
          <div class="live-config__test">
            <ui-radio-group v-if="!isGB" v-model="testStream" size="small" class="segmented-choice-group"
              ><ui-radio-button value="main">主码流</ui-radio-button><ui-radio-button value="sub">子码流</ui-radio-button></ui-radio-group
            >
            <ui-button size="small" type="primary" :loading="testing" @click="runTest">测试当前填写内容</ui-button>
          </div>
          <div v-if="testResult || lastTest" class="live-config__result">
            <template v-for="result in [testResult || lastTest]" :key="result.testedAt">
              <StatusDot :tone="resultTone(result)" :label="testStatusText[result.status] || result.status" />
              <p>{{ result.message }}</p>
              <small>{{ resultDetail(result) }} · {{ testResult ? '刚刚' : formatTime(result.testedAt) }}</small>
            </template>
          </div>
        </section>
      </ui-form>
    </div>
    <template #footer
      ><ui-button :disabled="saving" @click="visible = false">取消</ui-button
      ><ui-button type="primary" :loading="saving" :disabled="busy && !saving" @click="save">保存直播配置</ui-button></template
    >
  </ui-dialog>
</template>

<style scoped>
.live-config {
  display: grid;
  gap: var(--space-3);
}
.live-config__section {
  display: grid;
  gap: var(--space-2);
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
}
.live-config__section h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
}
.live-config__row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-6);
}
.live-config__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 0 var(--space-3);
}
.live-config__hint {
  margin: 0;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
}
.live-config__error {
  margin: 0;
  color: var(--danger-text);
  font-size: 12px;
}
.live-config__onvif {
  display: grid;
  gap: var(--space-2);
}
.live-config__test {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}
.live-config__result {
  display: grid;
  gap: 4px;
  padding: 10px 12px;
  background: var(--surface-muted);
  border-radius: 6px;
}
.live-config__result p {
  margin: 0;
  font-size: var(--font-size-sm);
  line-height: 1.6;
}
.live-config__result small {
  color: var(--text-muted);
  font-size: 12px;
}
.live-config :deep(.n-form-item) {
  margin-bottom: 0;
}
@media (max-width: 640px) {
  .live-config__section {
    padding: 12px;
  }
}
</style>
