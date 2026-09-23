import { Comment, Fragment, defineComponent, h, ref } from 'vue' /* 从业务插槽提取选择项和菜单项。 */
import { NCollapse, NCollapseItem, NConfigProvider, NDrawer, NDrawerContent, NDropdown, NModal, NPagination, NSelect, NStep, NSteps, NTabPane, NTabs, NTimePicker, NUpload, NUploadDragger, zhCN, dateZhCN } from 'naive-ui' /* 复合控件全部使用 Naive UI。 */

function nested(nodes, result = []) { /* 展开 Vue 条件节点与列表片段。 */
  for (const node of nodes || []) { /* 逐个访问插槽节点。 */
    if (!node || node.type === Comment) continue /* 忽略未显示的选项。 */
    if (Array.isArray(node)) { nested(node, result); continue } /* 递归读取数组节点。 */
    if (node.type === Fragment) { nested(node.children, result); continue } /* 递归读取 v-for。 */
    result.push(node) /* 保留有效组件节点。 */
  } /* 结束节点遍历。 */
  return result /* 返回扁平节点列表。 */
} /* 结束插槽展开。 */

export const UiOption = defineComponent({ /* 选项只供下拉框读取参数。 */
  name: 'UiOption', props: { label: String, value: [String, Number, Boolean], disabled: Boolean }, /* 保留业务选项。 */
  setup() { return () => null } /* 不单独输出节点。 */
}) /* 结束选项声明。 */

export const UiSelect = defineComponent({ /* 将选项子节点转换成 Naive UI 下拉选项。 */
  name: 'UiSelect', inheritAttrs: false, /* 明确转发样式与无障碍属性。 */
  props: { modelValue: [String, Number, Boolean, Array], multiple: Boolean, filterable: Boolean, clearable: Boolean, disabled: Boolean, placeholder: String, collapseTags: Boolean, collapseTagsTooltip: Boolean }, /* 保留现有选择器契约。 */
  emits: ['update:modelValue', 'change'], /* 保留业务选择回调。 */
  setup(props, { attrs, slots, emit }) { return () => { /* 每次渲染都读取动态选项。 */
    const booleanKey = value => value === true ? '__ui_boolean_true__' : value === false ? '__ui_boolean_false__' : value /* 布尔选项转成 Naive UI 接受的字符串键。 */
    const restoreValue = value => value === '__ui_boolean_true__' ? true : value === '__ui_boolean_false__' ? false : value /* 对业务继续返回布尔值。 */
    const options = nested(slots.default?.()).filter(node => node.type === UiOption || node.type?.name === 'UiOption').map(node => ({ label: node.props?.label ?? String(node.props?.value ?? ''), value: booleanKey(node.props?.value), disabled: node.props?.disabled })) /* 生成 Naive UI 所需选项。 */
    const selected = Array.isArray(props.modelValue) ? props.modelValue.map(booleanKey) : booleanKey(props.modelValue) /* 保持当前选择值的类型映射。 */
    return h(NSelect, { ...attrs, class: ['el-select', attrs.class], value: selected === '' ? null : selected, multiple: props.multiple, filterable: props.filterable, clearable: props.clearable, disabled: props.disabled, placeholder: props.placeholder, maxTagCount: props.collapseTags ? 'responsive' : undefined, options, 'onUpdate:value': value => { const next = value == null && !props.multiple ? '' : Array.isArray(value) ? value.map(restoreValue) : restoreValue(value); emit('update:modelValue', next); emit('change', next) } }) /* 绘制 Naive UI 下拉框。 */
  } } /* 结束选择器渲染。 */
}) /* 结束选择器适配。 */

export const UiDialog = defineComponent({ /* 模态框使用 Naive UI 卡片预设。 */
  name: 'UiDialog', inheritAttrs: false, /* 保留业务弹窗类名。 */
  props: { modelValue: Boolean, title: String, width: [String, Number], closeOnClickModal: { type: Boolean, default: true }, closeOnPressEscape: { type: Boolean, default: true }, showClose: { type: Boolean, default: true }, destroyOnClose: Boolean, top: String }, /* 保留弹窗行为。 */
  emits: ['update:modelValue', 'close', 'closed'], /* 保留关闭事件。 */
  setup(props, { attrs, slots, emit }) { return () => h(NModal, { ...attrs, class: ['el-dialog', attrs.class], show: props.modelValue, preset: 'card', title: props.title, style: { width: props.width || 'min(680px, 94vw)', maxWidth: '94vw', ...attrs.style }, maskClosable: props.closeOnClickModal, closeOnEsc: props.closeOnPressEscape, closable: props.showClose, displayDirective: props.destroyOnClose ? 'if' : 'show', 'onUpdate:show': value => emit('update:modelValue', value), onClose: () => emit('close'), onAfterLeave: () => emit('closed') }, slots) } /* 绘制 Naive UI 弹窗。 */
}) /* 结束弹窗适配。 */

export const UiDrawer = defineComponent({ /* 抽屉使用 Naive UI 的遮罩和焦点管理。 */
  name: 'UiDrawer', inheritAttrs: false, /* 保留页面类名。 */
  props: { modelValue: Boolean, title: String, size: [String, Number], direction: String, closeOnClickModal: { type: Boolean, default: true }, withHeader: { type: Boolean, default: true } }, /* 保留原抽屉参数。 */
  emits: ['update:modelValue', 'close'], /* 保留关闭回调。 */
  setup(props, { attrs, slots, emit }) { return () => h(NDrawer, { ...attrs, class: ['el-drawer', attrs.class], show: props.modelValue, width: props.size || 'min(680px, 94vw)', placement: props.direction === 'ltr' ? 'left' : props.direction === 'ttb' ? 'top' : props.direction === 'btt' ? 'bottom' : 'right', maskClosable: props.closeOnClickModal, 'onUpdate:show': value => { emit('update:modelValue', value); if (!value) emit('close') } }, { default: () => h(NDrawerContent, { title: props.withHeader ? props.title : undefined, closable: props.withHeader }, slots) }) } /* 关闭请求直接通知使用固定 model-value 的业务页面。 */
}) /* 结束抽屉适配。 */

export const UiPagination = defineComponent({ /* 分页仍以业务现有页码与总数为输入。 */
  name: 'UiPagination', inheritAttrs: false, /* 保留分页布局类名。 */
  props: { currentPage: { type: Number, default: 1 }, pageSize: { type: Number, default: 20 }, total: { type: Number, default: 0 }, pageSizes: Array, layout: String, background: Boolean, small: Boolean }, /* 保留分页参数。 */
  emits: ['update:currentPage', 'update:pageSize', 'current-change', 'size-change'], /* 保留页码与尺寸事件。 */
  setup(props, { attrs, emit }) { return () => h(NPagination, { ...attrs, class: ['el-pagination', attrs.class], page: props.currentPage, pageSize: props.pageSize, itemCount: props.total, pageSizes: props.pageSizes || [10, 20, 50, 100], showSizePicker: Boolean(props.pageSizes?.length), showQuickJumper: props.layout?.includes('jumper'), 'onUpdate:page': value => { emit('update:currentPage', value); emit('current-change', value) }, 'onUpdate:pageSize': value => { emit('update:pageSize', value); emit('size-change', value) } }) } /* 绘制 Naive UI 分页器。 */
}) /* 结束分页适配。 */

export const UiTabs = defineComponent({ /* 标签页使用 Naive UI，保持原页面筛选联动。 */
  name: 'UiTabs', inheritAttrs: false, /* 保留页面标签栏类名。 */
  props: { modelValue: [String, Number], type: String }, /* 当前标签页。 */
  emits: ['update:modelValue', 'tab-change', 'tab-click'], /* 保留旧业务事件。 */
  setup(props, { attrs, slots, emit }) { return () => h(NTabs, { ...attrs, class: ['el-tabs', attrs.class], value: props.modelValue, type: props.type === 'border-card' ? 'card' : 'line', size: 'small', 'onUpdate:value': value => { emit('update:modelValue', value); emit('tab-change', value); emit('tab-click', { props: { name: value } }) } }, slots) } /* 绘制 Naive UI 标签页。 */
}) /* 结束标签页适配。 */

export const UiTabPane = defineComponent({ /* 标签页内容直接交给 Naive UI。 */
  name: 'UiTabPane', inheritAttrs: false, /* 保留内容属性。 */
  props: { name: [String, Number], label: String, disabled: Boolean }, /* 标签名称和标题。 */
  setup(props, { attrs, slots }) { return () => h(NTabPane, { ...attrs, name: props.name || props.label, tab: props.label, disabled: props.disabled }, slots) } /* 绘制标签内容。 */
}) /* 结束标签内容适配。 */

export const UiCollapse = defineComponent({ /* 折叠区域使用 Naive UI。 */
  name: 'UiCollapse', inheritAttrs: false, /* 保留业务类名。 */
  setup(_, { attrs, slots }) { return () => h(NCollapse, { ...attrs, class: ['el-collapse', attrs.class] }, slots) } /* 绘制折叠容器。 */
}) /* 结束折叠容器适配。 */

export const UiCollapseItem = defineComponent({ /* 折叠项保留标题和标识。 */
  name: 'UiCollapseItem', inheritAttrs: false, /* 保留业务类名。 */
  props: { title: String, name: [String, Number] }, /* 折叠项信息。 */
  setup(props, { attrs, slots }) { return () => h(NCollapseItem, { ...attrs, title: props.title, name: props.name }, slots) } /* 绘制折叠项。 */
}) /* 结束折叠项适配。 */

export const UiSteps = defineComponent({ /* 步骤说明使用 Naive UI。 */
  name: 'UiSteps', inheritAttrs: false, /* 保留方向样式。 */
  props: { active: Number, direction: String }, /* 当前步骤与排列方向。 */
  setup(props, { attrs, slots }) { return () => h(NSteps, { ...attrs, current: props.active, vertical: props.direction === 'vertical' }, slots) } /* 绘制步骤容器。 */
}) /* 结束步骤容器适配。 */

export const UiStep = defineComponent({ /* 步骤标题和说明直接使用 Naive UI。 */
  name: 'UiStep', inheritAttrs: false, /* 保留步骤属性。 */
  props: { title: String, description: String }, /* 步骤内容。 */
  setup(props, { attrs, slots }) { return () => h(NStep, { ...attrs, title: props.title, description: props.description }, slots) } /* 绘制单步。 */
}) /* 结束单步适配。 */

export const UiTimePicker = defineComponent({ /* 静默时段继续使用 HH:mm 字符串。 */
  name: 'UiTimePicker', inheritAttrs: false, /* 保留输入样式。 */
  props: { modelValue: String, valueFormat: String, format: String, placeholder: String, clearable: Boolean, disabled: Boolean }, /* 保留业务时间字段。 */
  emits: ['update:modelValue', 'change'], /* 保留设置保存事件。 */
  setup(props, { attrs, emit }) { return () => h(NTimePicker, { ...attrs, class: ['el-time-editor', attrs.class], formattedValue: props.modelValue || null, format: props.format || 'HH:mm', placeholder: props.placeholder, clearable: props.clearable, disabled: props.disabled, 'onUpdate:formattedValue': value => { emit('update:modelValue', value || ''); emit('change', value || '') } }) } /* 绘制 Naive UI 时间选择器。 */
}) /* 结束时间选择器适配。 */

export const UiDropdownMenu = defineComponent({ name: 'UiDropdownMenu', setup() { return () => null } }) /* 菜单容器只供下拉组件读取。 */
export const UiDropdownItem = defineComponent({ name: 'UiDropdownItem', props: { command: String, disabled: Boolean }, setup() { return () => null } }) /* 菜单项只供下拉组件读取。 */

export const UiDropdown = defineComponent({ /* 账户菜单等悬浮菜单使用 Naive UI。 */
  name: 'UiDropdown', inheritAttrs: false, /* 保留触发元素属性。 */
  emits: ['command'], /* 保留账户操作回调。 */
  setup(_, { attrs, slots, emit }) { return () => { /* 根据下拉插槽生成 Naive UI 选项。 */
    const menus = nested(slots.dropdown?.()).flatMap(node => node.type === UiDropdownMenu || node.type?.name === 'UiDropdownMenu' ? nested(node.children?.default?.()) : [node]) /* 展开菜单容器。 */
    const options = menus.filter(node => node.type === UiDropdownItem || node.type?.name === 'UiDropdownItem').map(node => ({ key: node.props?.command, label: () => node.children?.default?.() || node.props?.command, disabled: node.props?.disabled })) /* 使用渲染函数保留菜单图标与文字。 */
    return h(NDropdown, { ...attrs, options, trigger: 'click', onSelect: key => emit('command', key) }, { default: () => slots.default?.() }) /* 绘制 Naive UI 下拉菜单。 */
  } } /* 结束菜单渲染。 */
}) /* 结束下拉菜单适配。 */

export const UiUpload = defineComponent({ /* 文件拖放交给 Naive UI，保留业务文件回调。 */
  name: 'UiUpload', inheritAttrs: false, /* 保留上传区域类名。 */
  props: { drag: Boolean, autoUpload: Boolean, disabled: Boolean, limit: Number, accept: String, onChange: Function, onRemove: Function, onExceed: Function }, /* 保留页面使用的上传参数。 */
  setup(props, { attrs, slots, expose }) { /* 保留上传列表清空能力。 */
    const upload = ref(null) /* 获取 Naive UI 上传实例。 */
    expose({ clearFiles: () => upload.value?.clear() }) /* 页面在校验失败或上传后清空文件。 */
    return () => h('div', { class: ['ui-upload', attrs.class] }, [h(NUpload, { ref: upload, disabled: props.disabled, max: props.limit, accept: props.accept, defaultUpload: props.autoUpload, onChange: ({ file }) => props.onChange?.({ raw: file?.file, size: file?.file?.size, name: file?.name }), onRemove: props.onRemove, onExceed: props.onExceed }, { default: () => props.drag ? h(NUploadDragger, null, { default: () => slots.default?.() }) : slots.default?.() }), slots.tip?.()]) /* 绘制拖放区域和提示。 */
  } /* 结束上传初始化。 */
}) /* 结束上传适配。 */

export const UiConfigProvider = defineComponent({ /* 全站统一 Naive UI 中文语言和主题。 */
  name: 'UiConfigProvider', inheritAttrs: false, /* 根布局自行管理类名。 */
  setup(_, { attrs, slots }) { return () => h(NConfigProvider, { ...attrs, locale: zhCN, dateLocale: dateZhCN, themeOverrides: { common: { primaryColor: '#18a058', primaryColorHover: '#36ad6a', primaryColorPressed: '#0c7a43', borderRadius: '6px' } } }, slots) } /* 使用模板风格的绿色主题。 */
}) /* 结束全站配置。 */
