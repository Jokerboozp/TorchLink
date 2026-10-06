import { defineComponent, h, ref } from 'vue'
import { NDatePicker, NTimePicker, NUpload, NUploadDragger } from 'naive-ui'

// 日期、时间与上传控件依赖较大（日期库、上传队列），单独成块，首次显示时再加载；
// composite-controls.js 以同名异步组件导出，页面用法不变。

export const UiTimePicker = defineComponent({
  /* 静默时段继续使用 HH:mm 字符串。 */
  name: 'UiTimePicker',
  inheritAttrs: false,
  props: { modelValue: String, valueFormat: String, format: String, placeholder: String, clearable: Boolean, disabled: Boolean },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    return () =>
      h(NTimePicker, {
        ...attrs,
        class: ['ui-time-picker', attrs.class],
        formattedValue: props.modelValue || null,
        format: props.format || 'HH:mm',
        placeholder: props.placeholder,
        clearable: props.clearable,
        disabled: props.disabled,
        'onUpdate:formattedValue': value => {
          emit('update:modelValue', value || '')
          emit('change', value || '')
        }
      })
  }
})

export const UiDateRange = defineComponent({
  /* 运维中心的自定义时间范围，值为毫秒时间戳数组。 */
  name: 'UiDateRange',
  inheritAttrs: false,
  props: { modelValue: Array, clearable: Boolean, disabled: Boolean, disableFuture: Boolean },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    return () =>
      h(NDatePicker, {
        ...attrs,
        class: ['ui-date-range', attrs.class],
        type: 'datetimerange',
        value: props.modelValue || null,
        clearable: props.clearable,
        disabled: props.disabled,
        isDateDisabled: props.disableFuture ? ts => ts > Date.now() : undefined,
        'onUpdate:value': value => {
          emit('update:modelValue', value || null)
          emit('change', value || null)
        }
      })
  }
})

export const UiDateTime = defineComponent({
  /* 单个日期时间，值为毫秒时间戳。 */
  name: 'UiDateTime',
  inheritAttrs: false,
  props: { modelValue: Number, clearable: Boolean, disabled: Boolean, disablePast: Boolean, disableFuture: Boolean, placeholder: String },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    return () =>
      h(NDatePicker, {
        ...attrs,
        class: ['ui-date-time', attrs.class],
        type: 'datetime',
        value: props.modelValue ?? null,
        clearable: props.clearable,
        disabled: props.disabled,
        placeholder: props.placeholder,
        isDateDisabled: props.disablePast ? ts => ts < Date.now() - 86400e3 : props.disableFuture ? ts => ts > Date.now() : undefined,
        'onUpdate:value': value => {
          emit('update:modelValue', value ?? null)
          emit('change', value ?? null)
        }
      })
  }
})

export const UiMonth = defineComponent({
  /* 月份选择，值为 yyyy-MM 字符串。 */
  name: 'UiMonth',
  inheritAttrs: false,
  props: { modelValue: String, clearable: Boolean, disabled: Boolean, placeholder: String },
  emits: ['update:modelValue', 'change'],
  setup(props, { attrs, emit }) {
    return () =>
      h(NDatePicker, {
        ...attrs,
        class: ['ui-month', attrs.class],
        type: 'month',
        valueFormat: 'yyyy-MM',
        formattedValue: props.modelValue || null,
        clearable: props.clearable,
        disabled: props.disabled,
        placeholder: props.placeholder,
        'onUpdate:formattedValue': value => {
          emit('update:modelValue', value || '')
          emit('change', value || '')
        }
      })
  }
})

export const UiUpload = defineComponent({
  name: 'UiUpload',
  inheritAttrs: false,
  props: {
    drag: Boolean,
    autoUpload: Boolean,
    disabled: Boolean,
    limit: Number,
    accept: String,
    onChange: Function,
    onRemove: Function,
    onExceed: Function
  },
  setup(props, { attrs, slots, expose }) {
    const upload = ref(null)
    expose({ clearFiles: () => upload.value?.clear() }) /* 页面在校验失败或上传后清空文件。 */
    return () =>
      h('div', { class: ['ui-upload', attrs.class] }, [
        h(
          NUpload,
          {
            ref: upload,
            disabled: props.disabled,
            max: props.limit,
            accept: props.accept,
            defaultUpload: props.autoUpload,
            onChange: ({ file }) => props.onChange?.({ raw: file?.file, size: file?.file?.size, name: file?.name }),
            onRemove: props.onRemove,
            onExceed: props.onExceed
          },
          { default: () => (props.drag ? h(NUploadDragger, null, { default: () => slots.default?.() }) : slots.default?.()) }
        ),
        slots.tip?.()
      ])
  }
})
