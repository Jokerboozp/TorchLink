import { ref } from 'vue'
import { api, isAbort, notifyError } from '../api'
import { useListLoader } from './useListLoader'

// 关联设备按关键字向服务端检索（最多 50 条），不预先加载全部设备：设备量大时
// 整表拉取会让页面长时间停在加载中。selected 返回当前已选设备编号，它不在
// 检索结果中时仍保留为选项，避免只显示编号或被清空。
export function useDeviceSearch(selected) {
  const devices = ref([])
  const loading = ref(false)
  const loader = useListLoader(loading)
  let timer = 0
  async function search(keyword = '') {
    try {
      const query = new URLSearchParams({ page: '1', pageSize: '50' })
      if (keyword.trim()) query.set('q', keyword.trim())
      const data = await loader.run(signal => api(`/api/v1/device-registry?${query}`, { signal }))
      const found = (data.items || []).map(item => item.device || item).filter(item => item.id)
      const id = selected()
      if (id && !found.some(item => item.id === id)) found.unshift({ id, name: id })
      devices.value = found
    } catch (error) {
      if (!isAbort(error)) notifyError(error)
    }
  }
  // 输入停顿 300 毫秒后再检索。
  function onSearch(keyword) {
    clearTimeout(timer)
    timer = setTimeout(() => search(keyword), 300)
  }
  function dispose() {
    clearTimeout(timer)
    loader.cancel()
  }
  return { devices, loading, search, onSearch, dispose }
}
