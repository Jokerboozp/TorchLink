import * as core from './core-controls'
import * as composite from './composite-controls'
import { UiTable, UiTableColumn } from './table'

function setLoading(element, value) { element.classList.toggle('ui-loading', Boolean(value)) }

const loadingDirective = { /* 代替旧组件库的 v-loading 指令。 */
  mounted: (element, binding) => setLoading(element, binding.value),
  updated: (element, binding) => setLoading(element, binding.value)
}

export function installUi(app) {
  for (const [name, component] of Object.entries({ ...core, ...composite, UiTable, UiTableColumn })) app.component(name, component) /* 以 ui- 前缀提供统一组件。 */
  app.directive('loading', loadingDirective)
}
