// Browser regression: open AI 工作流, then paste this read-only check into
// DevTools Console. Repeat at desktop, 1024x768 and 390x844, including the
// mobile 运行参数 panel. This needs real browser layout, not Node's DOM mocks.
(() => {
  const outer = document.querySelector('.main-content--ai')
  if (!outer) throw new Error('请先打开 AI 工作流页面')
  if (outer.scrollHeight > outer.clientHeight + 1 || outer.scrollTop !== 0) {
    throw new Error('AI 工作流外层仍可上下滚动')
  }
  const bounds = outer.getBoundingClientRect()
  for (const selector of ['.control-scroll', '.chat-log', '.chat-compose']) {
    const element = document.querySelector(selector)
    if (!element?.getClientRects().length) continue
    const rect = element.getBoundingClientRect()
    if (rect.height < 30 || rect.top < bounds.top || rect.bottom > bounds.bottom + 1) {
      throw new Error(`${selector} 被挤压或超出可视区域`)
    }
  }
  return { passed: true, outerHeight: outer.clientHeight, outerScrollHeight: outer.scrollHeight }
})()
