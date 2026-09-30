import { Comment, Fragment, defineComponent, h, ref } from 'vue'
import { NDataTable, NEmpty } from 'naive-ui'

// 窄屏不固定列：固定的操作列会遮住大半内容，改为随表格横向滚动。
const narrowQuery = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(max-width: 767px)') : null
const narrow = ref(Boolean(narrowQuery?.matches))
narrowQuery?.addEventListener?.('change', event => { narrow.value = event.matches })

export const UiTableColumn = defineComponent({
  name: 'UiTableColumn',
  props: ['type', 'prop', 'label', 'width', 'minWidth', 'align', 'fixed', 'showOverflowTooltip'],
  setup() { return () => null }
})

function collectColumns(nodes, result = []) {
  for (const node of nodes || []) {
    if (!node || node.type === Comment) continue
    if (Array.isArray(node)) { collectColumns(node, result); continue }
    if (node.type === Fragment) { collectColumns(node.children, result); continue }
    if (node.type === UiTableColumn || node.type?.name === 'UiTableColumn') result.push(node)
  }
  return result
}

function readCell(row, key) {
  return String(key || '').split('.').reduce((value, part) => value?.[part], row) ?? ''
}

export const UiTable = defineComponent({
  name: 'UiTable',
  inheritAttrs: false,
  props: {
    data: { type: Array, default: () => [] },
    stripe: Boolean,
    border: Boolean,
    rowKey: [String, Function],
    rowClassName: Function,
    emptyText: String,
    maxHeight: [String, Number],
    size: String,
    loading: Boolean
  },
  emits: ['selection-change'],
  setup(props, { attrs, slots, emit }) {
    return () => {
      const columns = collectColumns(slots.default?.()).map((node, index) => {
        const field = node.props || {}
        if (field.type === 'selection') return { type: 'selection', width: Number(field.width) || 48 }
        if (field.type === 'expand') return { type: 'expand', width: Number(field.width) || 48, renderExpand: (row, rowIndex) => node.children?.default?.({ row, $index: rowIndex }) }
        const column = {
          key: field.prop || `column-${index}`,
          title: field.label || '',
          width: field.width ? Number(field.width) : field.minWidth ? Number(field.minWidth) : undefined, /* 自动布局会忽略 min-width，把最小宽度作为列的基准宽度，避免短列被挤成多行。 */
          minWidth: field.minWidth ? Number(field.minWidth) : undefined,
          align: field.align || 'left',
          fixed: narrow.value ? undefined : field.fixed || undefined,
          ellipsis: field.showOverflowTooltip ? { tooltip: true } : undefined,
          render: (row, rowIndex) => node.children?.default ? node.children.default({ row, $index: rowIndex }) : readCell(row, field.prop)
        }
        return column
      })
      const scrollX = columns.reduce((total, column) => total + (column.width || column.minWidth || 140), 0)
      return h(NDataTable, {
        ...attrs,
        class: ['ui-table', attrs.class],
        columns,
        data: props.data,
        striped: props.stripe,
        bordered: props.border,
        maxHeight: props.maxHeight,
        size: props.size === 'small' ? 'small' : 'medium',
        scrollX,
        tableLayout: 'fixed',
        rowKey: row => typeof props.rowKey === 'function' ? props.rowKey(row) : row?.[props.rowKey || 'id'] ?? row?.messageId ?? props.data.indexOf(row),
        rowClassName: props.rowClassName ? (row, index) => props.rowClassName({ row, rowIndex: index }) : undefined,
        'onUpdate:checkedRowKeys': (keys, rows) => emit('selection-change', rows || props.data.filter(row => keys.includes(row?.[props.rowKey || 'id'] ?? row?.messageId))),
        loading: props.loading
      }, { empty: () => h(NEmpty, { description: props.emptyText || '暂无数据', size: 'small' }) })
    }
  }
})
