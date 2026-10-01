import * as core from './core-controls' /* 导入基于 Naive UI 的基础业务控件。 */
import * as composite from './composite-controls' /* 导入基于 Naive UI 的复合业务控件。 */
import { UiTable, UiTableColumn } from './table' /* 导入基于 Naive UI 的数据表格。 */

function setLoading(element, value) { element.classList.toggle('ui-loading', Boolean(value)) } /* 标记表格或卡片的等待状态。 */

export const loadingDirective = { /* 代替旧组件库的 v-loading 指令。 */
  mounted: (element, binding) => setLoading(element, binding.value), /* 初次渲染时设置等待状态。 */
  updated: (element, binding) => setLoading(element, binding.value) /* 数据变化时同步等待状态。 */
} /* 结束等待指令。 */

export function installUi(app) { /* 在所有业务页面中注册 Naive UI 控件。 */
  for (const [name, component] of Object.entries({ ...core, ...composite, UiTable, UiTableColumn })) app.component(name, component) /* 以 ui- 前缀提供统一组件。 */
  app.directive('loading', loadingDirective) /* 保留现有页面的加载提示用法。 */
} /* 结束组件注册。 */
