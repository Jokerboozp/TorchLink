import { Comment, Fragment, defineComponent, h } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NCheckbox,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NProgress,
  NRadio,
  NRadioButton,
  NRadioGroup,
  NSkeleton,
  NSwitch,
  NTag,
  NTooltip
} from 'naive-ui'

const kind = value =>
  ({ danger: 'error', error: 'error', primary: 'primary', success: 'success', warning: 'warning', info: 'info' })[value] ||
  'default' /* 对齐两套组件的状态名称。 */

export const UiButton = defineComponent({
  name: 'UiButton',
  inheritAttrs: false /* 明确转发原生属性。 */,
  props: {
    type: String,
    plain: Boolean,
    link: Boolean,
    text: Boolean,
    round: Boolean,
    circle: Boolean,
    size: String,
    loading: Boolean,
    disabled: Boolean,
    nativeType: String
  },
  setup(props, { attrs, slots }) {
    return () =>
      h(
        NButton,
        {
          ...attrs,
          class: ['ui-button', (props.link || props.text) && 'ui-button--text', attrs.class],
          type: kind(props.type),
          secondary: props.plain,
          text: props.link || props.text,
          round: props.round,
          circle: props.circle,
          size: props.size || 'medium',
          loading: props.loading,
          disabled: props.disabled,
          attrType: props.nativeType || 'button'
        },
        slots
      )
  }
})

export const UiInput = defineComponent({
  name: 'UiInput',
  inheritAttrs: false /* 保留 aria 与键盘事件。 */,
  props: {
    modelValue: [String, Number],
    type: String,
    rows: Number,
    autosize: [Boolean, Object],
    placeholder: String,
    disabled: Boolean,
    readonly: Boolean,
    clearable: Boolean,
    maxlength: [String, Number],
    showPassword: Boolean,
    resize: String
  },
  emits: ['update:modelValue', 'change', 'input', 'clear'],
  setup(props, { attrs, emit, slots }) {
    return () => {
      const { id, name, autocomplete, required, 'aria-label': ariaLabel, ...rootAttrs } = attrs
      return h(
        NInput,
        {
          ...rootAttrs,
          inputProps: { id, name, autocomplete, required, 'aria-label': ariaLabel },
          class: ['ui-input', attrs.class],
          value: props.modelValue == null ? '' : String(props.modelValue),
          type: props.type === 'textarea' ? 'textarea' : props.type === 'password' ? 'password' : 'text',
          rows: props.rows,
          autosize: props.autosize,
          placeholder: props.placeholder,
          disabled: props.disabled,
          readonly: props.readonly,
          clearable: props.clearable,
          maxlength: props.maxlength,
          showPasswordOn: props.showPassword ? 'mousedown' : undefined,
          'onUpdate:value': value => {
            emit('update:modelValue', value)
            emit('input', value)
            if (value === '') emit('clear')
          },
          onChange: value => emit('change', value)
        },
        slots
      )
    }
  } /* 将 Naive UI 输入值交回业务表单；id、name 与 aria-label 落在原生输入框上。 */
})

export const UiInputNumber = defineComponent({
  name: 'UiInputNumber',
  inheritAttrs: false,
  props: { modelValue: Number, min: Number, max: Number, step: Number, precision: Number, disabled: Boolean, placeholder: String },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    return () => {
      const { id, name, required, 'aria-label': ariaLabel, ...rootAttrs } = attrs
      return h(NInputNumber, {
        ...rootAttrs,
        inputProps: { id, name, required, 'aria-label': ariaLabel },
        class: ['ui-input-number', attrs.class],
        value: props.modelValue,
        min: props.min,
        max: props.max,
        step: props.step,
        precision: props.precision,
        disabled: props.disabled,
        placeholder: props.placeholder,
        'onUpdate:value': value => {
          emit('update:modelValue', value)
          emit('change', value)
        }
      })
    }
  }
})

export const UiCard = defineComponent({
  name: 'UiCard',
  inheritAttrs: false,
  props: { shadow: String, header: String },
  setup(props, { attrs, slots }) {
    return () => h(NCard, { ...attrs, class: ['ui-card', attrs.class], title: props.header, bordered: true, size: 'medium' }, slots)
  }
})

export const UiTag = defineComponent({
  name: 'UiTag',
  inheritAttrs: false,
  props: { type: String, round: Boolean, effect: String, size: String },
  setup(props, { attrs, slots }) {
    return () =>
      h(
        NTag,
        {
          ...attrs,
          class: ['ui-tag', attrs.class],
          type: kind(props.type),
          round: props.round,
          bordered: props.effect === 'plain',
          size: props.size || 'small'
        },
        slots
      )
  }
})

export const UiAlert = defineComponent({
  name: 'UiAlert',
  inheritAttrs: false,
  props: { title: String, description: String, type: String, closable: { type: Boolean, default: true }, showIcon: Boolean },
  setup(props, { attrs, slots }) {
    return () =>
      h(
        NAlert,
        {
          ...attrs,
          class: ['ui-alert', attrs.class],
          title: props.title,
          type: kind(props.type) === 'default' ? 'info' : kind(props.type),
          closable: props.closable
        },
        { header: () => slots.title?.() || props.title, default: () => slots.default?.() || props.description }
      )
  }
})

export const UiEmpty = defineComponent({
  name: 'UiEmpty',
  inheritAttrs: false,
  props: { description: String },
  setup(props, { attrs, slots }) {
    return () => h(NEmpty, { ...attrs, class: ['ui-empty', attrs.class], description: props.description }, slots)
  }
})

export const UiForm = defineComponent({
  name: 'UiForm',
  inheritAttrs: false,
  props: { model: Object, labelPosition: String, inline: Boolean, disabled: Boolean },
  setup(props, { attrs, slots }) {
    return () =>
      h(
        NForm,
        {
          ...attrs,
          class: ['ui-form', props.inline && 'ui-form-inline', attrs.class],
          model: props.model,
          labelPlacement: props.labelPosition === 'top' ? 'top' : 'left',
          disabled: props.disabled,
          showFeedback: false
        },
        slots
      )
  }
})

export const UiFormItem = defineComponent({
  name: 'UiFormItem',
  inheritAttrs: false,
  // error 非空时在字段下方显示该提示并标红，页面自行决定何时校验。
  props: { label: String, prop: String, required: Boolean, error: String },
  setup(props, { attrs, slots }) {
    return () =>
      h(
        NFormItem,
        {
          ...attrs,
          class: ['ui-form-item', attrs.class],
          label: props.label,
          path: props.prop,
          required: props.required,
          showFeedback: Boolean(props.error),
          feedback: props.error || undefined,
          validationStatus: props.error ? 'error' : undefined
        },
        slots
      )
  }
})

export const UiDescriptions = defineComponent({
  name: 'UiDescriptions',
  inheritAttrs: false,
  props: { column: [Number, Object], border: Boolean, size: String },
  setup(props, { attrs, slots }) {
    return () => {
      /* 把业务包装字段展开成 Naive UI 直接子项。 */
      const items = []
      const visit = nodes => {
        for (const node of nodes || []) {
          if (!node || node.type === Comment) continue
          if (Array.isArray(node)) {
            visit(node)
            continue
          }
          if (node.type === Fragment) {
            visit(node.children)
            continue
          }
          if (node.type === UiDescriptionsItem || node.type?.name === 'UiDescriptionsItem')
            items.push(
              h(NDescriptionsItem, { label: node.props?.label, span: node.props?.span }, { default: () => node.children?.default?.() })
            )
        }
      }
      visit(slots.default?.())
      return h(
        NDescriptions,
        {
          ...attrs,
          class: ['ui-descriptions', attrs.class],
          column: props.column || 1,
          bordered: props.border,
          size: props.size || 'small',
          labelPlacement: 'left'
        },
        { default: () => items }
      )
    }
  }
})

export const UiDescriptionsItem = defineComponent({
  name: 'UiDescriptionsItem',
  inheritAttrs: false,
  props: { label: String, span: Number },
  setup(props, { attrs, slots }) {
    return () => h(NDescriptionsItem, { ...attrs, label: props.label, span: props.span }, slots)
  }
})

export const UiRadioGroup = defineComponent({
  name: 'UiRadioGroup',
  inheritAttrs: false,
  props: { modelValue: [String, Number, Boolean], size: String, disabled: Boolean },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, slots, emit }) {
    return () =>
      h(
        NRadioGroup,
        {
          ...attrs,
          class: ['ui-radio-group', attrs.class],
          value: props.modelValue,
          size: props.size || 'medium',
          disabled: props.disabled,
          'onUpdate:value': value => {
            emit('update:modelValue', value)
            emit('change', value)
          }
        },
        slots
      )
  }
})

export const UiRadioButton = defineComponent({
  // Naive 的单选组按子组件名称 RadioButton 识别按钮组，名称不同会丢失按钮组高度与分隔线。
  name: 'RadioButton',
  inheritAttrs: false,
  props: { value: [String, Number, Boolean], label: String, disabled: Boolean },
  setup(props, { attrs, slots }) {
    return () => h(NRadioButton, { ...attrs, value: props.value ?? props.label, disabled: props.disabled }, slots)
  }
})

export const UiRadio = defineComponent({
  name: 'UiRadio',
  inheritAttrs: false,
  props: { value: [String, Number, Boolean], label: String, disabled: Boolean },
  setup(props, { attrs, slots }) {
    return () => h(NRadio, { ...attrs, value: props.value ?? props.label, disabled: props.disabled }, slots)
  }
})

export const UiCheckbox = defineComponent({
  name: 'UiCheckbox',
  inheritAttrs: false,
  props: { modelValue: Boolean, disabled: Boolean, label: String },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, slots, emit }) {
    return () =>
      h(
        NCheckbox,
        {
          ...attrs,
          class: ['ui-checkbox', attrs.class],
          checked: props.modelValue,
          disabled: props.disabled,
          'onUpdate:checked': value => {
            emit('update:modelValue', value)
            emit('change', value)
          }
        },
        { default: () => slots.default?.() || props.label }
      )
  }
})

export const UiSwitch = defineComponent({
  name: 'UiSwitch',
  inheritAttrs: false,
  props: {
    modelValue: [Boolean, String, Number],
    disabled: Boolean,
    activeText: String,
    inactiveText: String,
    activeValue: { type: [Boolean, String, Number], default: true },
    inactiveValue: { type: [Boolean, String, Number], default: false }
  } /* 同时支持设备状态字符串和普通布尔值。 */,
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    return () =>
      h('span', { class: ['ui-switch-field', attrs.class] }, [
        h(NSwitch, {
          disabled: props.disabled,
          value: props.modelValue,
          checkedValue: props.activeValue,
          uncheckedValue: props.inactiveValue,
          'onUpdate:value': value => {
            emit('update:modelValue', value)
            emit('change', value)
          }
        }),
        props.activeText || props.inactiveText
          ? h(
              'span',
              { class: 'ui-switch-label' },
              /* 只给 active-text 时它是开关的固定标签，关闭时也显示。 */
              props.modelValue === props.activeValue || props.inactiveText === undefined ? props.activeText : props.inactiveText
            )
          : null
      ])
  }
})

export const UiProgress = defineComponent({
  name: 'UiProgress',
  inheritAttrs: false,
  props: { percentage: Number, status: String, strokeWidth: Number, showText: { type: Boolean, default: true } },
  setup(props, { attrs }) {
    return () =>
      h(NProgress, {
        ...attrs,
        class: ['ui-progress', attrs.class],
        type: 'line',
        percentage: props.percentage || 0,
        status: props.status === 'exception' ? 'error' : props.status || 'default',
        height: props.strokeWidth || 10,
        showIndicator: props.showText
      })
  }
})

export const UiSkeleton = defineComponent({
  name: 'UiSkeleton',
  inheritAttrs: false,
  props: { rows: { type: Number, default: 1 }, animated: Boolean, loading: { type: Boolean, default: true } },
  setup(props, { attrs, slots }) {
    return () =>
      props.loading
        ? h(
            'div',
            { ...attrs, class: ['ui-skeleton', attrs.class] },
            Array.from({ length: props.rows }, (_, index) => h(NSkeleton, { key: index, text: true, repeat: 1, animated: props.animated }))
          )
        : slots.default?.()
  }
})

export const UiTooltip = defineComponent({
  name: 'UiTooltip',
  inheritAttrs: false,
  props: { content: String, placement: String },
  setup(props, { attrs, slots }) {
    return () =>
      h(
        NTooltip,
        { ...attrs, placement: props.placement || 'top' },
        { default: () => slots.content?.() || props.content, trigger: () => slots.default?.() }
      )
  }
})
