// Browser regression: open 智能助手, then paste this read-only check into
// DevTools Console. Repeat at desktop, 1024x768 and 390x844, including the
// mobile 运行参数 panel. This needs real browser layout, not Node's DOM mocks.
(() => { /* 执行当前语句并推进处理流程。 */
  const outer = document.querySelector('.main-content--ai') /* 声明 outer。 */
  if (!outer) throw new Error('请先打开 智能助手页面') /* 判断条件并选择处理分支。 */
  if (outer.scrollHeight > outer.clientHeight + 1 || outer.scrollTop !== 0) { /* 判断条件并选择处理分支。 */
    throw new Error('智能助手外层仍可上下滚动') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const bounds = outer.getBoundingClientRect() /* 声明 bounds。 */
  for (const selector of ['.control-scroll', '.chat-log', '.chat-compose']) { /* 循环处理当前数据。 */
    const element = document.querySelector(selector) /* 声明 element。 */
    if (!element?.getClientRects().length) continue /* 判断条件并选择处理分支。 */
    const rect = element.getBoundingClientRect() /* 声明 rect。 */
    if (rect.height < 30 || rect.top < bounds.top || rect.bottom > bounds.bottom + 1) { /* 判断条件并选择处理分支。 */
      throw new Error(`${selector} 被挤压或超出可视区域`) /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
  return { passed: true, outerHeight: outer.clientHeight, outerScrollHeight: outer.scrollHeight } /* 返回当前处理结果。 */
})() /* 结束当前表达式或代码块。 */
