import { Comment, Fragment, defineComponent, h, ref } from 'vue' /* 使用 Vue 节点保留各页面的单元格插槽。 */
import { NDataTable, NEmpty } from 'naive-ui' /* 表格绘制、固定列与选择能力由 Naive UI 提供。 */

// 窄屏不固定列：固定的操作列会遮住大半内容，改为随表格横向滚动。
const narrowQuery = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(max-width: 767px)') : null
const narrow = ref(Boolean(narrowQuery?.matches))
narrowQuery?.addEventListener?.('change', event => { narrow.value = event.matches })

export const UiTableColumn = defineComponent({ /* 列声明只向父表格提供配置，不单独产生页面节点。 */
  name: 'UiTableColumn', /* 供父表格识别列节点。 */
  props: ['type', 'prop', 'label', 'width', 'minWidth', 'align', 'fixed', 'showOverflowTooltip'], /* 沿用业务列的语义。 */
  setup() { return () => null } /* 列内容通过父表格的插槽渲染。 */
}) /* 结束列声明。 */

function collectColumns(nodes, result = []) { /* 展开 v-for 与条件节点，保持列定义顺序。 */
  for (const node of nodes || []) { /* 逐个读取页面提供的列。 */
    if (!node || node.type === Comment) continue /* 忽略条件隐藏的占位节点。 */
    if (Array.isArray(node)) { collectColumns(node, result); continue } /* 递归读取数组形式的列。 */
    if (node.type === Fragment) { collectColumns(node.children, result); continue } /* 递归读取 v-for 片段。 */
    if (node.type === UiTableColumn || node.type?.name === 'UiTableColumn') result.push(node) /* 收集有效列。 */
  } /* 结束列遍历。 */
  return result /* 返回当前表格的列。 */
} /* 结束列收集。 */

function readCell(row, key) { /* 普通属性列支持点分隔的对象路径。 */
  return String(key || '').split('.').reduce((value, part) => value?.[part], row) ?? '' /* 缺失值显示为空。 */
} /* 结束单元格读取。 */

export const UiTable = defineComponent({ /* 把页面列插槽转换为 Naive UI 数据表格列。 */
  name: 'UiTable', /* 提供稳定的组件名称。 */
  inheritAttrs: false, /* 把样式类与属性明确传给表格根节点。 */
  props: { /* 保留业务表格正在使用的输入契约。 */
    data: { type: Array, default: () => [] }, /* 表格数据。 */
    stripe: Boolean, /* 交替行背景。 */
    border: Boolean, /* 单元格边线。 */
    rowKey: [String, Function], /* 选择和展开行使用的标识。 */
    rowClassName: Function, /* 业务状态驱动的行样式。 */
    emptyText: String, /* 空列表提示。 */
    maxHeight: [String, Number], /* 最大可视高度。 */
    size: String, /* 表格密度。 */
    loading: Boolean /* 加载中显示表格自带的等待状态。 */
  }, /* 结束输入契约。 */
  emits: ['selection-change'], /* 继续把勾选行交给业务模块。 */
  setup(props, { attrs, slots, emit }) { /* 初始化表格适配逻辑。 */
    return () => { /* 数据或列发生变化时重建 Naive UI 列配置。 */
      const columns = collectColumns(slots.default?.()).map((node, index) => { /* 保留声明顺序与插槽。 */
        const field = node.props || {} /* 读取当前列参数。 */
        if (field.type === 'selection') return { type: 'selection', width: Number(field.width) || 48 } /* 用原生选择列替代旧控件。 */
        if (field.type === 'expand') return { type: 'expand', width: Number(field.width) || 48, renderExpand: (row, rowIndex) => node.children?.default?.({ row, $index: rowIndex }) } /* 用原生展开列承载详情插槽。 */
        const column = { /* 生成普通列的显示参数。 */
          key: field.prop || `column-${index}`, /* 给 Naive UI 稳定的列标识。 */
          title: field.label || '', /* 保留表头文字。 */
          width: field.width ? Number(field.width) : field.minWidth ? Number(field.minWidth) : undefined, /* 自动布局会忽略 min-width，把最小宽度作为列的基准宽度，避免短列被挤成多行。 */
          minWidth: field.minWidth ? Number(field.minWidth) : undefined, /* 保留最小宽度。 */
          align: field.align || 'left', /* 保留对齐方式。 */
          fixed: narrow.value ? undefined : field.fixed || undefined, /* 宽屏保留固定操作列。 */
          ellipsis: field.showOverflowTooltip ? { tooltip: true } : undefined, /* 长文本显示悬浮提示。 */
          render: (row, rowIndex) => node.children?.default ? node.children.default({ row, $index: rowIndex }) : readCell(row, field.prop) /* 保留业务单元格插槽。 */
        } /* 结束普通列配置。 */
        return column /* 返回当前列。 */
      }) /* 结束列转换。 */
      const scrollX = columns.reduce((total, column) => total + (column.width || column.minWidth || 140), 0) /* 宽表格允许横向滚动。 */
      return h(NDataTable, { /* 渲染 Naive UI 数据表格。 */
        ...attrs, /* 保留页面的类名与无障碍属性。 */
        class: ['ui-table', attrs.class], /* 兼容已有布局样式。 */
        columns, /* 传入转换后的列。 */
        data: props.data, /* 传入当前页数据。 */
        striped: props.stripe, /* 按页面配置显示斑马纹。 */
        bordered: props.border, /* 按页面配置显示边框。 */
        maxHeight: props.maxHeight, /* 限制长表格高度。 */
        size: props.size === 'small' ? 'small' : 'medium', /* 保持紧凑的管理页密度。 */
        scrollX, /* 保证窄屏仍可访问所有列。 */
        tableLayout: 'fixed', /* 按列声明的宽度布局，长标识不再挤压短列。 */
        rowKey: row => typeof props.rowKey === 'function' ? props.rowKey(row) : row?.[props.rowKey || 'id'] ?? row?.messageId ?? props.data.indexOf(row), /* 保留行标识。 */
        rowClassName: props.rowClassName ? (row, index) => props.rowClassName({ row, rowIndex: index }) : undefined, /* 兼容业务行样式回调。 */
        'onUpdate:checkedRowKeys': (keys, rows) => emit('selection-change', rows || props.data.filter(row => keys.includes(row?.[props.rowKey || 'id'] ?? row?.messageId))), /* 将选择键还原为业务行。 */
        loading: props.loading /* 表格自身的加载状态。 */
      }, { empty: () => h(NEmpty, { description: props.emptyText || '暂无数据', size: 'small' }) }) /* 空列表显示当前页面给出的提示。 */
    } /* 结束渲染函数。 */
  } /* 结束表格初始化。 */
}) /* 结束数据表格适配。 */
