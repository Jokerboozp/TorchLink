import { Comment, Fragment, defineComponent, h } from 'vue' /* 使用统一适配组件承接现有业务的双向绑定。 */
import { NAlert, NButton, NCard, NCheckbox, NDescriptions, NDescriptionsItem, NEmpty, NForm, NFormItem, NInput, NInputNumber, NProgress, NRadio, NRadioButton, NRadioGroup, NSkeleton, NSwitch, NTag, NTooltip } from 'naive-ui' /* 页面控件全部由 Naive UI 绘制。 */

const kind = value => ({ danger: 'error', primary: 'primary', success: 'success', warning: 'warning', info: 'info' })[value] || 'default' /* 对齐两套组件的状态名称。 */

export const UiButton = defineComponent({ /* 保持原按钮事件与视觉语义。 */
  name: 'UiButton', inheritAttrs: false, /* 明确转发原生属性。 */
  props: { type: String, plain: Boolean, link: Boolean, text: Boolean, round: Boolean, circle: Boolean, size: String, loading: Boolean, disabled: Boolean, nativeType: String }, /* 页面按钮常用参数。 */
  setup(props, { attrs, slots }) { return () => h(NButton, { ...attrs, class: ['ui-button', (props.link || props.text) && 'ui-button--text', attrs.class], type: kind(props.type), secondary: props.plain, text: props.link || props.text, round: props.round, circle: props.circle, size: props.size || 'medium', loading: props.loading, disabled: props.disabled, attrType: props.nativeType || 'button' }, slots) } /* 使用 Naive UI 按钮。 */
}) /* 结束按钮适配。 */

export const UiInput = defineComponent({ /* 文本、密码与多行输入共用 Naive UI 输入框。 */
  name: 'UiInput', inheritAttrs: false, /* 保留 aria 与键盘事件。 */
  props: { modelValue: [String, Number], type: String, rows: Number, autosize: [Boolean, Object], placeholder: String, disabled: Boolean, readonly: Boolean, clearable: Boolean, maxlength: [String, Number], showPassword: Boolean, resize: String }, /* 保留业务输入参数。 */
  emits: ['update:modelValue', 'change', 'input', 'clear'], /* 保持现有输入事件。 */
  setup(props, { attrs, emit, slots }) { return () => { const { id, name, autocomplete, required, 'aria-label': ariaLabel, ...rootAttrs } = attrs; return h(NInput, { ...rootAttrs, inputProps: { id, name, autocomplete, required, 'aria-label': ariaLabel }, class: ['ui-input', attrs.class], value: props.modelValue == null ? '' : String(props.modelValue), type: props.type === 'textarea' ? 'textarea' : props.type === 'password' ? 'password' : 'text', rows: props.rows, autosize: props.autosize, placeholder: props.placeholder, disabled: props.disabled, readonly: props.readonly, clearable: props.clearable, maxlength: props.maxlength, showPasswordOn: props.showPassword ? 'mousedown' : undefined, 'onUpdate:value': value => { emit('update:modelValue', value); emit('input', value); if (value === '') emit('clear') }, onChange: value => emit('change', value) }, slots) } } /* 将 Naive UI 输入值交回业务表单；id、name 与 aria-label 落在原生输入框上。 */
}) /* 结束文本输入适配。 */

export const UiInputNumber = defineComponent({ /* 数字输入保留最小值、精度和更新事件。 */
  name: 'UiInputNumber', inheritAttrs: false, /* 保留无障碍属性。 */
  props: { modelValue: Number, min: Number, max: Number, step: Number, precision: Number, disabled: Boolean, placeholder: String }, /* 数字输入参数。 */
  emits: ['update:modelValue', 'change'], /* 保持原有业务回调。 */
  setup(props, { attrs, emit }) { return () => { const { id, name, required, 'aria-label': ariaLabel, ...rootAttrs } = attrs; return h(NInputNumber, { ...rootAttrs, inputProps: { id, name, required, 'aria-label': ariaLabel }, class: ['ui-input-number', attrs.class], value: props.modelValue, min: props.min, max: props.max, step: props.step, precision: props.precision, disabled: props.disabled, placeholder: props.placeholder, 'onUpdate:value': value => { emit('update:modelValue', value); emit('change', value) } }) } } /* 渲染 Naive UI 数字输入。 */
}) /* 结束数字输入适配。 */

export const UiCard = defineComponent({ /* 管理卡片采用模板同款简洁边框。 */
  name: 'UiCard', inheritAttrs: false, /* 保留业务卡片类名。 */
  props: { shadow: String, header: String }, /* 接受原有卡片参数。 */
  setup(props, { attrs, slots }) { return () => h(NCard, { ...attrs, class: ['ui-card', attrs.class], title: props.header, bordered: true, size: 'medium' }, slots) } /* 将页头和正文插槽交给 Naive UI。 */
}) /* 结束卡片适配。 */

export const UiTag = defineComponent({ /* 状态标签统一使用 Naive UI。 */
  name: 'UiTag', inheritAttrs: false, /* 保留页面上的额外样式。 */
  props: { type: String, round: Boolean, effect: String, size: String }, /* 保留状态与尺寸。 */
  setup(props, { attrs, slots }) { return () => h(NTag, { ...attrs, class: ['ui-tag', attrs.class], type: kind(props.type), round: props.round, bordered: props.effect === 'plain', size: props.size || 'small' }, slots) } /* 绘制状态标签。 */
}) /* 结束标签适配。 */

export const UiAlert = defineComponent({ /* 告警提示统一使用 Naive UI。 */
  name: 'UiAlert', inheritAttrs: false, /* 保留页面自定义类。 */
  props: { title: String, description: String, type: String, closable: { type: Boolean, default: true }, showIcon: Boolean }, /* 保留提示标题和内容。 */
  setup(props, { attrs, slots }) { return () => h(NAlert, { ...attrs, class: ['ui-alert', attrs.class], title: props.title, type: kind(props.type) === 'default' ? 'info' : kind(props.type), closable: props.closable }, { header: () => slots.title?.() || props.title, default: () => slots.default?.() || props.description }) } /* 保留业务使用的标题插槽和正文。 */
}) /* 结束提示适配。 */

export const UiEmpty = defineComponent({ /* 空状态统一使用 Naive UI。 */
  name: 'UiEmpty', inheritAttrs: false, /* 保留布局类名。 */
  props: { description: String }, /* 保留空状态说明。 */
  setup(props, { attrs, slots }) { return () => h(NEmpty, { ...attrs, class: ['ui-empty', attrs.class], description: props.description }, slots) } /* 绘制空状态。 */
}) /* 结束空状态适配。 */

export const UiForm = defineComponent({ /* 表单使用 Naive UI 的标签和校验结构。 */
  name: 'UiForm', inheritAttrs: false, /* 保留表单提交监听。 */
  props: { model: Object, labelPosition: String, inline: Boolean, disabled: Boolean }, /* 保留页面使用的表单选项。 */
  setup(props, { attrs, slots }) { return () => h(NForm, { ...attrs, class: ['ui-form', props.inline && 'ui-form-inline', attrs.class], model: props.model, labelPlacement: props.labelPosition === 'top' ? 'top' : 'left', disabled: props.disabled, showFeedback: false }, slots) } /* 绘制表单。 */
}) /* 结束表单适配。 */

export const UiFormItem = defineComponent({ /* 表单字段保持原有标签文案。 */
  name: 'UiFormItem', inheritAttrs: false, /* 保留字段类名。 */
  props: { label: String, prop: String, required: Boolean }, /* 接收字段说明。 */
  setup(props, { attrs, slots }) { return () => h(NFormItem, { ...attrs, class: ['ui-form-item', attrs.class], label: props.label, path: props.prop, required: props.required, showFeedback: false }, slots) } /* 绘制字段布局。 */
}) /* 结束字段适配。 */

export const UiDescriptions = defineComponent({ /* 详情键值列表直接使用 Naive UI。 */
  name: 'UiDescriptions', inheritAttrs: false, /* 保留详情布局类。 */
  props: { column: [Number, Object], border: Boolean, size: String }, /* 保留列数和边框。 */
  setup(props, { attrs, slots }) { return () => { /* 把业务包装字段展开成 Naive UI 直接子项。 */
    const items = [] /* 收集条件字段和循环字段。 */
    const visit = nodes => { for (const node of nodes || []) { if (!node || node.type === Comment) continue; if (Array.isArray(node)) { visit(node); continue }; if (node.type === Fragment) { visit(node.children); continue }; if (node.type === UiDescriptionsItem || node.type?.name === 'UiDescriptionsItem') items.push(h(NDescriptionsItem, { label: node.props?.label, span: node.props?.span }, { default: () => node.children?.default?.() })) } } /* 展开字段声明。 */
    visit(slots.default?.()) /* 读取当前显示的字段。 */
    return h(NDescriptions, { ...attrs, class: ['ui-descriptions', attrs.class], column: props.column || 1, bordered: props.border, size: props.size || 'small', labelPlacement: 'left' }, { default: () => items }) /* 绘制详情列表。 */
  } } /* 结束详情渲染。 */
}) /* 结束详情适配。 */

export const UiDescriptionsItem = defineComponent({ /* 详情字段直接映射 Naive UI。 */
  name: 'UiDescriptionsItem', inheritAttrs: false, /* 保留字段类名。 */
  props: { label: String, span: Number }, /* 保留字段标签和跨度。 */
  setup(props, { attrs, slots }) { return () => h(NDescriptionsItem, { ...attrs, label: props.label, span: props.span }, slots) } /* 绘制字段内容。 */
}) /* 结束详情字段适配。 */

export const UiRadioGroup = defineComponent({ /* 单选分组保留 v-model 语义。 */
  name: 'UiRadioGroup', inheritAttrs: false, /* 保留无障碍标签。 */
  props: { modelValue: [String, Number, Boolean], size: String, disabled: Boolean }, /* 当前选项和尺寸。 */
  emits: ['update:modelValue', 'change'], /* 保留业务变更事件。 */
  setup(props, { attrs, slots, emit }) { return () => h(NRadioGroup, { ...attrs, class: ['ui-radio-group', attrs.class], value: props.modelValue, size: props.size || 'medium', disabled: props.disabled, 'onUpdate:value': value => { emit('update:modelValue', value); emit('change', value) } }, slots) } /* 绘制单选组。 */
}) /* 结束单选组适配。 */

export const UiRadioButton = defineComponent({ /* 分段式选项使用 Naive UI 单选按钮。 */
  name: 'UiRadioButton', inheritAttrs: false, /* 保留选项属性。 */
  props: { value: [String, Number, Boolean], label: String, disabled: Boolean }, /* 当前选项信息。 */
  setup(props, { attrs, slots }) { return () => h(NRadioButton, { ...attrs, value: props.value ?? props.label, disabled: props.disabled }, slots) } /* 绘制单选按钮。 */
}) /* 结束单选按钮适配。 */

export const UiRadio = defineComponent({ /* 普通单选项使用 Naive UI。 */
  name: 'UiRadio', inheritAttrs: false, /* 保留选项属性。 */
  props: { value: [String, Number, Boolean], label: String, disabled: Boolean }, /* 当前选项信息。 */
  setup(props, { attrs, slots }) { return () => h(NRadio, { ...attrs, value: props.value ?? props.label, disabled: props.disabled }, slots) } /* 绘制单选项。 */
}) /* 结束单选项适配。 */

export const UiCheckbox = defineComponent({ /* 独立勾选框保留布尔双向绑定。 */
  name: 'UiCheckbox', inheritAttrs: false, /* 保留权限选项样式。 */
  props: { modelValue: Boolean, disabled: Boolean, label: String }, /* 当前勾选状态。 */
  emits: ['update:modelValue', 'change'], /* 保留权限编辑回调。 */
  setup(props, { attrs, slots, emit }) { return () => h(NCheckbox, { ...attrs, class: ['ui-checkbox', attrs.class], checked: props.modelValue, disabled: props.disabled, 'onUpdate:checked': value => { emit('update:modelValue', value); emit('change', value) } }, { default: () => slots.default?.() || props.label }) } /* 绘制勾选框。 */
}) /* 结束勾选框适配。 */

export const UiSwitch = defineComponent({ /* 开关保留现有布尔值语义。 */
  name: 'UiSwitch', inheritAttrs: false, /* 保留业务样式。 */
  props: { modelValue: [Boolean, String, Number], disabled: Boolean, activeText: String, inactiveText: String, activeValue: { type: [Boolean, String, Number], default: true }, inactiveValue: { type: [Boolean, String, Number], default: false } }, /* 同时支持设备状态字符串和普通布尔值。 */
  emits: ['update:modelValue', 'change'], /* 保留设置变更回调。 */
  setup(props, { attrs, emit }) { return () => h('span', { class: ['ui-switch-field', attrs.class] }, [h(NSwitch, { disabled: props.disabled, value: props.modelValue, checkedValue: props.activeValue, uncheckedValue: props.inactiveValue, 'onUpdate:value': value => { emit('update:modelValue', value); emit('change', value) } }), props.activeText || props.inactiveText ? h('span', { class: 'ui-switch-label' }, props.modelValue === props.activeValue ? props.activeText : props.inactiveText) : null]) } /* 绘制开关与文字。 */
}) /* 结束开关适配。 */

export const UiProgress = defineComponent({ /* 进度条使用 Naive UI。 */
  name: 'UiProgress', inheritAttrs: false, /* 保留页面样式。 */
  props: { percentage: Number, status: String, strokeWidth: Number, showText: { type: Boolean, default: true } }, /* 保留进度与状态。 */
  setup(props, { attrs }) { return () => h(NProgress, { ...attrs, class: ['ui-progress', attrs.class], type: 'line', percentage: props.percentage || 0, status: props.status === 'exception' ? 'error' : props.status || 'default', height: props.strokeWidth || 10, showIndicator: props.showText }) } /* 绘制进度条。 */
}) /* 结束进度条适配。 */

export const UiSkeleton = defineComponent({ /* 加载占位行由 Naive UI 骨架屏组成。 */
  name: 'UiSkeleton', inheritAttrs: false, /* 保留外层样式。 */
  props: { rows: { type: Number, default: 1 }, animated: Boolean, loading: { type: Boolean, default: true } }, /* 保留占位行数和加载状态。 */
  setup(props, { attrs, slots }) { return () => props.loading ? h('div', { ...attrs, class: ['ui-skeleton', attrs.class] }, Array.from({ length: props.rows }, (_, index) => h(NSkeleton, { key: index, text: true, repeat: 1, animated: props.animated }))) : slots.default?.() } /* 加载结束后恢复页面内容。 */
}) /* 结束骨架屏适配。 */

export const UiTooltip = defineComponent({ /* 提示内容保留原触发元素插槽。 */
  name: 'UiTooltip', inheritAttrs: false, /* 保留位置与样式属性。 */
  props: { content: String, placement: String }, /* 提示文字与方向。 */
  setup(props, { attrs, slots }) { return () => h(NTooltip, { ...attrs, placement: props.placement || 'top' }, { default: () => slots.content?.() || props.content, trigger: () => slots.default?.() }) } /* 绘制 Naive UI 浮层提示。 */
}) /* 结束提示适配。 */
