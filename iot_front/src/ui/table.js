import { Comment, Fragment, defineComponent, h, ref } from 'vue'
import { NDataTable, NEmpty } from 'naive-ui'

// 窄屏不固定列：固定的操作列会遮住大半内容，改为随表格横向滚动。
const narrowQuery = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(max-width: 767px)') : null
const narrow = ref(Boolean(narrowQuery?.matches))
narrowQuery?.addEventListener?.('change', event => {
  narrow.value = event.matches
})

export const UiTableColumn = defineComponent({
  /* 列声明只向父表格提供配置，不单独产生页面节点。 */
  name: 'UiTableColumn',
  props: ['type', 'prop', 'label', 'width', 'minWidth', 'align', 'fixed', 'showOverflowTooltip'],
  setup() {
    return () => null
  }
})

function collectColumns(nodes, result = []) {
  /* 展开 v-for 与条件节点，保持列定义顺序。 */
  for (const node of nodes || []) {
    if (!node || node.type === Comment) continue
    if (Array.isArray(node)) {
      collectColumns(node, result)
      continue
    }
    if (node.type === Fragment) {
      collectColumns(node.children, result)
      continue
    }
    if (node.type === UiTableColumn || node.type?.name === 'UiTableColumn') result.push(node)
  }
  return result
}

function readCell(row, key) {
  /* 普通属性列支持点分隔的对象路径。 */
  return (
    String(key || '')
      .split('.')
      .reduce((value, part) => value?.[part], row) ?? ''
  )
}

export const UiTable = defineComponent({
  name: 'UiTable',
  inheritAttrs: false /* 把样式类与属性明确传给表格根节点。 */,
  props: {
    /* 保留业务表格正在使用的输入契约。 */
    data: { type: Array, default: () => [] },
    stripe: Boolean,
    border: Boolean,
    rowKey: [String, Function],
    rowClassName: Function,
    emptyText: String,
    maxHeight: [String, Number],
    size: String,
    loading: Boolean,
    // 窄屏时是否改为卡片排列。
    cards: { type: Boolean, default: true }
  },
  emits: ['selection-change'],
  setup(props, { attrs, slots, emit }) {
    function renderCards(columns) {
      const rowKey = (row, index) =>
        typeof props.rowKey === 'function' ? props.rowKey(row) : (row?.[props.rowKey || 'id'] ?? row?.messageId ?? index)
      const body = props.data.length
        ? props.data.map((row, index) => {
            const extra = attrs.rowProps?.(row, index) || {}
            const className = props.rowClassName?.({ row, rowIndex: index })
            const fields = columns.filter(column => column.title)
            const actions = columns.filter(column => !column.title)
            return h('article', { ...extra, key: rowKey(row, index), class: ['ui-table-card', className, extra.class] }, [
              h(
                'dl',
                fields.map(column =>
                  h('div', { class: 'ui-table-card__field' }, [h('dt', column.title), h('dd', column.render(row, index))])
                )
              ),
              ...actions.map(column => h('div', { class: 'ui-table-card__actions' }, column.render(row, index)))
            ])
          })
        : [h(NEmpty, { description: props.loading ? '加载中' : props.emptyText || '暂无数据', size: 'small' })]
      return h('div', { class: ['ui-table', 'ui-table-cards', attrs.class], 'aria-busy': props.loading ? 'true' : undefined }, body)
    }
    return () => {
      const columns = collectColumns(slots.default?.()).map((node, index) => {
        const field = node.props || {}
        if (field.type === 'selection') return { type: 'selection', width: Number(field.width) || 48 }
        if (field.type === 'expand')
          return {
            type: 'expand',
            width: Number(field.width) || 48,
            renderExpand: (row, rowIndex) => node.children?.default?.({ row, $index: rowIndex })
          }
        const column = {
          key: field.prop || `column-${index}`,
          title: field.label || '',
          width: field.width
            ? Number(field.width)
            : field.minWidth
              ? Number(field.minWidth)
              : undefined /* 自动布局会忽略 min-width，把最小宽度作为列的基准宽度，避免短列被挤成多行。 */,
          minWidth: field.minWidth ? Number(field.minWidth) : undefined,
          align: field.align || 'left',
          fixed: narrow.value ? undefined : field.fixed || undefined,
          ellipsis: field.showOverflowTooltip ? { tooltip: true } : undefined,
          render: (row, rowIndex) => (node.children?.default ? node.children.default({ row, $index: rowIndex }) : readCell(row, field.prop))
        }
        return column
      })
      // 窄屏把普通表格按卡片排列，每行显示“列名：值”，无列名的操作列放在卡片底部；
      // 带勾选或展开列的表格仍横向滚动，保持原有交互。
      if (narrow.value && props.cards && !columns.some(column => column.type)) return renderCards(columns)
      const scrollX = columns.reduce((total, column) => total + (column.width || column.minWidth || 140), 0)
      return h(
        NDataTable,
        {
          ...attrs,
          class: ['ui-table', attrs.class],
          columns,
          data: props.data,
          striped: props.stripe,
          bordered: props.border,
          maxHeight: props.maxHeight,
          size: props.size === 'small' ? 'small' : 'medium',
          scrollX,
          tableLayout: 'fixed' /* 按列声明的宽度布局，长标识不再挤压短列。 */,
          rowKey: row =>
            typeof props.rowKey === 'function'
              ? props.rowKey(row)
              : (row?.[props.rowKey || 'id'] ?? row?.messageId ?? props.data.indexOf(row)),
          rowClassName: props.rowClassName
            ? (row, index) => props.rowClassName({ row, rowIndex: index })
            : undefined /* 兼容业务行样式回调。 */,
          'onUpdate:checkedRowKeys': (keys, rows) =>
            emit(
              'selection-change',
              rows || props.data.filter(row => keys.includes(row?.[props.rowKey || 'id'] ?? row?.messageId))
            ) /* 将选择键还原为业务行。 */,
          loading: props.loading
        },
        { empty: () => h(NEmpty, { description: props.emptyText || '暂无数据', size: 'small' }) }
      )
    }
  }
})
