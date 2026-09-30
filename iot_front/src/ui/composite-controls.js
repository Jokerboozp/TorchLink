import { Comment, Fragment, defineComponent, h, ref } from 'vue'
import { themeOverrides } from '../theme/naive.js'
import { NCollapse, NCollapseItem, NConfigProvider, NDatePicker, NDrawer, NDrawerContent, NDropdown, NModal, NPagination, NSelect, NStep, NSteps, NTabPane, NTabs, NTimePicker, NUpload, NUploadDragger, zhCN, dateZhCN } from 'naive-ui'

function nested(nodes, result = []) {
  for (const node of nodes || []) {
    if (!node || node.type === Comment) continue
    if (Array.isArray(node)) { nested(node, result); continue }
    if (node.type === Fragment) { nested(node.children, result); continue }
    result.push(node)
  }
  return result
}

export const UiOption = defineComponent({
  name: 'UiOption', props: { label: String, value: [String, Number, Boolean], disabled: Boolean },
  setup() { return () => null }
})

export const UiSelect = defineComponent({
  name: 'UiSelect', inheritAttrs: false,
  props: { modelValue: [String, Number, Boolean, Array], multiple: Boolean, filterable: Boolean, clearable: Boolean, disabled: Boolean, placeholder: String, collapseTags: Boolean, collapseTagsTooltip: Boolean, allowCreate: Boolean },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, slots, emit }) { return () => {
    const booleanKey = value => value === true ? '__ui_boolean_true__' : value === false ? '__ui_boolean_false__' : value /* 布尔选项转成 Naive UI 接受的字符串键。 */
    const restoreValue = value => value === '__ui_boolean_true__' ? true : value === '__ui_boolean_false__' ? false : value
    const options = nested(slots.default?.()).filter(node => node.type === UiOption || node.type?.name === 'UiOption').map(node => ({ label: node.props?.label ?? String(node.props?.value ?? ''), value: booleanKey(node.props?.value), disabled: node.props?.disabled }))
    const selected = Array.isArray(props.modelValue) ? props.modelValue.map(booleanKey) : booleanKey(props.modelValue)
    return h(NSelect, { ...attrs, class: ['ui-select', attrs.class], value: selected === '' ? null : selected, multiple: props.multiple, filterable: props.filterable, clearable: props.clearable, disabled: props.disabled, placeholder: props.placeholder, tag: props.allowCreate, maxTagCount: props.collapseTags ? 'responsive' : undefined, options, 'onUpdate:value': value => { const next = value == null && !props.multiple ? '' : Array.isArray(value) ? value.map(restoreValue) : restoreValue(value); emit('update:modelValue', next); emit('change', next) } })
  } }
})

export const UiDialog = defineComponent({
  name: 'UiDialog', inheritAttrs: false,
  props: { modelValue: Boolean, title: String, width: [String, Number], closeOnClickModal: { type: Boolean, default: true }, closeOnPressEscape: { type: Boolean, default: true }, showClose: { type: Boolean, default: true }, destroyOnClose: Boolean, top: String },
  emits: ['update:modelValue', 'close', 'closed'],
  setup(props, { attrs, slots, emit }) { return () => h(NModal, { ...attrs, class: ['ui-dialog', attrs.class], show: props.modelValue, preset: 'card', title: props.title, style: { width: props.width || 'min(680px, 94vw)', maxWidth: '94vw', ...attrs.style }, maskClosable: props.closeOnClickModal, closeOnEsc: props.closeOnPressEscape, closable: props.showClose, displayDirective: props.destroyOnClose ? 'if' : 'show', 'onUpdate:show': value => emit('update:modelValue', value), onClose: () => emit('close'), onAfterLeave: () => emit('closed') }, slots) }
})

export const UiDrawer = defineComponent({
  name: 'UiDrawer', inheritAttrs: false,
  props: { modelValue: Boolean, title: String, size: [String, Number], direction: String, closeOnClickModal: { type: Boolean, default: true }, withHeader: { type: Boolean, default: true } },
  emits: ['update:modelValue', 'close'],
  setup(props, { attrs, slots, emit }) { return () => h(NDrawer, { ...attrs, class: ['ui-drawer', attrs.class], show: props.modelValue, width: props.size || 'min(680px, 94vw)', placement: props.direction === 'ltr' ? 'left' : props.direction === 'ttb' ? 'top' : props.direction === 'btt' ? 'bottom' : 'right', maskClosable: props.closeOnClickModal, 'onUpdate:show': value => { emit('update:modelValue', value); if (!value) emit('close') } }, { default: () => h(NDrawerContent, { title: props.withHeader ? props.title : undefined, closable: props.withHeader }, slots) }) } /* 关闭请求直接通知使用固定 model-value 的业务页面。 */
})

export const UiPagination = defineComponent({
  name: 'UiPagination', inheritAttrs: false,
  props: { currentPage: { type: Number, default: 1 }, pageSize: { type: Number, default: 20 }, total: { type: Number, default: 0 }, pageSizes: Array, layout: String, background: Boolean, small: Boolean },
  emits: ['update:currentPage', 'update:pageSize', 'current-change', 'size-change'],
  setup(props, { attrs, emit }) { return () => h(NPagination, { ...attrs, class: ['ui-pagination', attrs.class], page: props.currentPage, pageSize: props.pageSize, itemCount: props.total, pageSizes: props.pageSizes || [10, 20, 50, 100], showSizePicker: Boolean(props.pageSizes?.length), showQuickJumper: props.layout?.includes('jumper'), 'onUpdate:page': value => { emit('update:currentPage', value); emit('current-change', value) }, 'onUpdate:pageSize': value => { emit('update:pageSize', value); emit('size-change', value) } }) }
})

export const UiTabs = defineComponent({
  name: 'UiTabs', inheritAttrs: false,
  props: { modelValue: [String, Number], type: String },
  emits: ['update:modelValue', 'tab-change', 'tab-click'],
  setup(props, { attrs, slots, emit }) { return () => {
    const panes = nested(slots.default?.()).filter(node => node.type === UiTabPane || node.type?.name === 'UiTabPane').map(node => { const { label, ...paneProps } = node.props || {}; return h(NTabPane, { ...paneProps, name: paneProps.name || label, tab: label }, { ...(node.children?.default ? { default: node.children.default } : {}), tab: () => label }) }) /* 明确提供页签标题插槽，避免空白页签。 */
    return h(NTabs, { ...attrs, class: ['ui-tabs', attrs.class], value: props.modelValue, type: props.type === 'border-card' ? 'card' : 'line', size: 'medium', 'onUpdate:value': value => { emit('update:modelValue', value); emit('tab-change', value); emit('tab-click', { props: { name: value } }) } }, { default: () => panes })
  } }
})

export const UiTabPane = defineComponent({
  name: 'UiTabPane', inheritAttrs: false,
  props: { name: [String, Number], label: String, disabled: Boolean },
  setup(props, { attrs, slots }) { return () => h(NTabPane, { ...attrs, name: props.name || props.label, tab: props.label, disabled: props.disabled }, slots) }
})

export const UiCollapse = defineComponent({
  name: 'UiCollapse', inheritAttrs: false,
  setup(_, { attrs, slots }) { return () => h(NCollapse, { ...attrs, class: ['ui-collapse', attrs.class] }, slots) }
})

export const UiCollapseItem = defineComponent({
  name: 'UiCollapseItem', inheritAttrs: false,
  props: { title: String, name: [String, Number] },
  setup(props, { attrs, slots }) { return () => h(NCollapseItem, { ...attrs, title: props.title, name: props.name }, slots) }
})

export const UiSteps = defineComponent({
  name: 'UiSteps', inheritAttrs: false,
  props: { active: Number, direction: String },
  setup(props, { attrs, slots }) { return () => h(NSteps, { ...attrs, current: props.active, vertical: props.direction === 'vertical' }, slots) }
})

export const UiStep = defineComponent({
  name: 'UiStep', inheritAttrs: false,
  props: { title: String, description: String },
  setup(props, { attrs, slots }) { return () => h(NStep, { ...attrs, title: props.title, description: props.description }, slots) }
})

export const UiTimePicker = defineComponent({
  name: 'UiTimePicker', inheritAttrs: false,
  props: { modelValue: String, valueFormat: String, format: String, placeholder: String, clearable: Boolean, disabled: Boolean },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) { return () => h(NTimePicker, { ...attrs, class: ['ui-time-picker', attrs.class], formattedValue: props.modelValue || null, format: props.format || 'HH:mm', placeholder: props.placeholder, clearable: props.clearable, disabled: props.disabled, 'onUpdate:formattedValue': value => { emit('update:modelValue', value || ''); emit('change', value || '') } }) }
})

export const UiDateRange = defineComponent({
  name: 'UiDateRange', inheritAttrs: false,
  props: { modelValue: Array, clearable: Boolean, disabled: Boolean, disableFuture: Boolean },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) { return () => h(NDatePicker, { ...attrs, class: ['ui-date-range', attrs.class], type: 'datetimerange', value: props.modelValue || null, clearable: props.clearable, disabled: props.disabled, isDateDisabled: props.disableFuture ? ts => ts > Date.now() : undefined, 'onUpdate:value': value => { emit('update:modelValue', value || null); emit('change', value || null) } }) }
})

export const UiDateTime = defineComponent({
  name: 'UiDateTime', inheritAttrs: false,
  props: { modelValue: Number, clearable: Boolean, disabled: Boolean, disablePast: Boolean, disableFuture: Boolean, placeholder: String },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) { return () => h(NDatePicker, { ...attrs, class: ['ui-date-time', attrs.class], type: 'datetime', value: props.modelValue ?? null, clearable: props.clearable, disabled: props.disabled, placeholder: props.placeholder, isDateDisabled: props.disablePast ? ts => ts < Date.now() - 86400e3 : props.disableFuture ? ts => ts > Date.now() : undefined, 'onUpdate:value': value => { emit('update:modelValue', value ?? null); emit('change', value ?? null) } }) }
})

export const UiDropdownMenu = defineComponent({ name: 'UiDropdownMenu', setup() { return () => null } })
export const UiDropdownItem = defineComponent({ name: 'UiDropdownItem', props: { command: String, disabled: Boolean }, setup() { return () => null } })

export const UiDropdown = defineComponent({
  name: 'UiDropdown', inheritAttrs: false,
  emits: ['command'],
  setup(_, { attrs, slots, emit }) { return () => {
    const menus = nested(slots.dropdown?.()).flatMap(node => node.type === UiDropdownMenu || node.type?.name === 'UiDropdownMenu' ? nested(node.children?.default?.()) : [node])
    const options = menus.filter(node => node.type === UiDropdownItem || node.type?.name === 'UiDropdownItem').map(node => ({ key: node.props?.command, label: () => h('span', { class: 'ui-dropdown-label' }, node.children?.default?.() || node.props?.command), disabled: node.props?.disabled }))
    const { class: triggerClass, style: triggerStyle, ...dropdownAttrs } = attrs /* 触发器布局类不能传到弹出的菜单。 */
    return h('div', { class: triggerClass, style: triggerStyle }, [h(NDropdown, { ...dropdownAttrs, options, trigger: 'click', onSelect: key => emit('command', key) }, { default: () => slots.default?.() })])
  } }
})

export const UiUpload = defineComponent({
  name: 'UiUpload', inheritAttrs: false,
  props: { drag: Boolean, autoUpload: Boolean, disabled: Boolean, limit: Number, accept: String, onChange: Function, onRemove: Function, onExceed: Function },
  setup(props, { attrs, slots, expose }) {
    const upload = ref(null)
    expose({ clearFiles: () => upload.value?.clear() })
    return () => h('div', { class: ['ui-upload', attrs.class] }, [h(NUpload, { ref: upload, disabled: props.disabled, max: props.limit, accept: props.accept, defaultUpload: props.autoUpload, onChange: ({ file }) => props.onChange?.({ raw: file?.file, size: file?.file?.size, name: file?.name }), onRemove: props.onRemove, onExceed: props.onExceed }, { default: () => props.drag ? h(NUploadDragger, null, { default: () => slots.default?.() }) : slots.default?.() }), slots.tip?.()])
  }
})

export const UiConfigProvider = defineComponent({
  name: 'UiConfigProvider', inheritAttrs: false,
  setup(_, { attrs, slots }) { return () => h(NConfigProvider, { ...attrs, locale: zhCN, dateLocale: dateZhCN, themeOverrides }, slots) }
})
