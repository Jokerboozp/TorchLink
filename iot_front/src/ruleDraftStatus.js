export function reconcileRuleDraftMessages(messages, rules) { /* 执行当前语句并推进处理流程。 */
  const currentById = new Map((Array.isArray(rules) ? rules : []).filter(rule => rule?.id).map(rule => [rule.id, rule])) /* 声明 currentById。 */
  let updated = 0 /* 声明 updated。 */
  for (const message of Array.isArray(messages) ? messages : []) { /* 循环处理当前数据。 */
    const id = message?.ruleDraft?.id /* 声明 id。 */
    if (!id || message.ruleDraftPersisted !== true) continue /* 判断条件并选择处理分支。 */
    const current = currentById.get(id) /* 声明 current。 */
    if (!current) { /* 判断条件并选择处理分支。 */
      message.ruleDraftState = 'missing' /* 更新 message.ruleDraftState 的值。 */
      updated++ /* 执行当前语句并推进处理流程。 */
      continue /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    message.ruleDraft = { ...message.ruleDraft, ...current } /* 更新 message.ruleDraft 的值。 */
    message.ruleDraftState = current.enabled === true ? 'enabled' : 'draft' /* 更新 message.ruleDraftState 的值。 */
    updated++ /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return updated /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
