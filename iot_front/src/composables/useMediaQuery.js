import { onBeforeUnmount, ref } from 'vue'

// 在没有 window 的环境（如单元测试）中始终返回 false，不影响页面逻辑。
export function useMediaQuery(query) {
  const media = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia(query) : null
  const matches = ref(Boolean(media?.matches))
  if (!media) return matches
  const update = event => { matches.value = event.matches }
  media.addEventListener('change', update)
  onBeforeUnmount(() => media.removeEventListener('change', update))
  return matches
}
