// 知识库页面共用的智能体、分类与索引状态判断。

export function agentKey(item) {
  return item?.id || item?.workflowId || ''
}
export function agentName(item) {
  return item?.name || item?.label || agentKey(item) || '未命名智能体'
}
const categoryNames = {
  manual: '设备手册',
  'alarm-sop': '告警处置操作规程',
  maintenance: '运维维修',
  regulation: '消防规范',
  faq: '常见问题'
}
export function categoryLabel(value) {
  return categoryNames[value] || value || '未分类'
}
export function isIndexing(document) {
  return ['PENDING', 'UPLOADED', 'INDEXING', 'DELETING'].includes(document?.status)
}
// Failed documents, and documents waiting for an automatic retry, can be retried now.
export const retryable = document =>
  document?.status === 'INDEX_FAILED' || (document?.status === 'UPLOADED' && Boolean(document.metadata?.indexRetryAt))
