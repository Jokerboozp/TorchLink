// Completion and source coverage are separate: matching stored artifacts does
// not prove that an expired PostgreSQL or ClickHouse source was reconstructed.
export function restoreSummary(result) {
  const status = result?.status || 'UNKNOWN'
  const application = result?.components?.application
  const objects = result?.components?.applicationObjects
  const records = Object.values(result?.kinds || {}).reduce((sum, item) => sum + Number(item.restored || 0), 0)
  const lines = [`独立恢复库已写入 ${records} 条报文记录`]
  if (application?.status === 'restored') {
    lines.push('固定分析、业务版本及权限关联已核对')
    if (application.retiredExecutions > 0) lines.push(`${application.retiredExecutions} 项待执行或运行中任务已退役，须人工新建任务`)
  } else if (application?.status === 'not_included') lines.push('本备份未包含固定分析与业务版本')
  else lines.push('固定分析与业务版本恢复范围未确认')
  if (objects?.status === 'restored') {
    lines.push(`${objects.objects || 0} 份应用附件已恢复并校验`)
    if (objects.originalHashUnknown > 0) lines.push(`${objects.originalHashUnknown} 份旧附件缺少原始上传哈希，本次按备份制品校验`)
  }
  const limitations = Array.isArray(application?.limitations) ? application.limitations : []
  return {
    status,
    tone: status === 'COMPLETED' ? 'success' : status === 'PARTIAL' ? 'warning' : 'error',
    title: status === 'COMPLETED' ? '本备份恢复校验完成' : status === 'PARTIAL' ? '已恢复，仍有来源覆盖限制' : '恢复校验未完成',
    lines,
    limitations,
  }
}
