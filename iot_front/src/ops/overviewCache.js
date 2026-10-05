// 运维总览最近一次结果，只保存在当前页面内存中，按租户和用户隔离。
// 再次进入总览时先显示上次结果并在后台刷新，不必等全部组件和指标重新检查完。
import { session } from '../api'

let cached = null

const owner = () => `${session.tenant}\u0000${session.user}`

export function overviewSnapshot() {
  if (!cached || cached.owner !== owner())
    cached = { owner: owner(), components: {}, kpiGroups: {}, jobs: null, trends: {}, trendRange: '', checkedAt: 0 }
  return cached
}
