<script setup>
// 直播弹窗：关闭即销毁播放器并释放会话；同一时间只播放一个摄像头。
import { computed } from 'vue'
import LivePlayer from './LivePlayer.vue'
import { cameraLocation } from '../liveVideo'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  camera: { type: Object, default: null }
})
const emit = defineEmits(['update:modelValue'])
const visible = computed({ get: () => props.modelValue && Boolean(props.camera?.cameraId), set: value => emit('update:modelValue', value) })
</script>

<template>
  <ui-dialog v-model="visible" class="live-player-dialog" title="摄像头直播" width="min(880px, 96vw)" destroy-on-close>
    <LivePlayer
      v-if="visible"
      :key="camera.cameraId"
      :camera-id="camera.cameraId"
      :camera-name="camera.cameraName"
      :location="cameraLocation(camera)"
    />
    <template #footer><ui-button @click="visible = false">关闭</ui-button></template>
  </ui-dialog>
</template>
