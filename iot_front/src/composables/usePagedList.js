import { ref } from 'vue'

// 服务端分页列表：翻页时读取该页，改每页条数时回到第一页再读取。页码与每页条数由页面提供
// （通常同时交给 usePageState 记住），这里只集中翻页规则，读取仍由页面的 load 完成。
export function usePagedList(load, { page = ref(1), pageSize = ref(20) } = {}) {
  function changePage(value) {
    page.value = value
    return load()
  }
  function changePageSize(value) {
    pageSize.value = value
    page.value = 1
    return load()
  }
  return { page, pageSize, changePage, changePageSize }
}
