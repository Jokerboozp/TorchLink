export function reconcileRuleDraftMessages(messages, rules) {
  const currentById = new Map((Array.isArray(rules) ? rules : []).filter(rule => rule?.id).map(rule => [rule.id, rule]))
  let updated = 0
  for (const message of Array.isArray(messages) ? messages : []) {
    const id = message?.ruleDraft?.id
    if (!id || message.ruleDraftPersisted !== true) continue
    const current = currentById.get(id)
    if (!current) {
      message.ruleDraftState = 'missing'
      updated++
      continue
    }
    message.ruleDraft = { ...message.ruleDraft, ...current }
    message.ruleDraftState = current.enabled === true ? 'enabled' : 'draft'
    updated++
  }
  return updated
}

// 对话中规则草稿卡片的状态标签与操作：已启用或已删除的规则不再进入编辑。
export function ruleDraftView(message) {
  const state = message?.ruleDraftState
  const persisted = Boolean(message?.ruleDraftPersisted)
  if (state === 'enabled') return { label: '已启用', type: 'success', action: '规则已启用', disabled: true }
  if (state === 'missing') return { label: '已删除', type: 'info', action: '规则已删除', disabled: true }
  return persisted
    ? { label: '已保存草稿', type: 'warning', action: '查看并启用规则', disabled: false }
    : { label: '待人工确认', type: 'warning', action: '检查并保存规则', disabled: false }
}
