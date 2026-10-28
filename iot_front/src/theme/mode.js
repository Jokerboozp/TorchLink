import { computed, ref } from 'vue'

// 外观偏好只保存在本浏览器：light、dark 或 system（跟随操作系统）。
const storageKey = 'iot:theme'
const media = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null
const systemDark = ref(Boolean(media?.matches))
media?.addEventListener?.('change', event => {
  systemDark.value = event.matches
  apply()
})

function stored() {
  try {
    const value = localStorage.getItem(storageKey)
    return ['light', 'dark', 'system'].includes(value) ? value : 'light'
  } catch {
    return 'light'
  }
}

export const themeMode = ref(stored())
export const isDark = computed(() => themeMode.value === 'dark' || (themeMode.value === 'system' && systemDark.value))

function apply() {
  if (typeof document === 'undefined') return
  document.documentElement.dataset.theme = isDark.value ? 'dark' : 'light'
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', isDark.value ? '#1f1e1c' : '#faf9f5')
}

export function setThemeMode(value) {
  if (!['light', 'dark', 'system'].includes(value)) return
  themeMode.value = value
  try {
    localStorage.setItem(storageKey, value)
  } catch {
    // 无法保存时仅本次生效。
  }
  apply()
}

apply()
