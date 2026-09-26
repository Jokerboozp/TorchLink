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
