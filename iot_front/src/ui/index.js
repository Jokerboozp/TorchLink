import * as core from './core-controls'
import * as composite from './composite-controls'
import { UiTable, UiTableColumn } from './table'

function setLoading(element, value) { element.classList.toggle('ui-loading', Boolean(value)) }

const loadingDirective = {
  mounted: (element, binding) => setLoading(element, binding.value),
  updated: (element, binding) => setLoading(element, binding.value)
}

export function installUi(app) {
  for (const [name, component] of Object.entries({ ...core, ...composite, UiTable, UiTableColumn })) app.component(name, component)
  app.directive('loading', loadingDirective)
}
