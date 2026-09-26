// Alertmanager 返回完整快照；只让当前页进入组件树，分组内的告警也共享页容量。
export function sortAlerts(alerts = []) {
  return alerts.map(alert => ({ alert, time: Date.parse(alert.startsAt) || 0 }))
    .sort((a, b) => b.time - a.time || String(a.alert.fingerprint).localeCompare(String(b.alert.fingerprint)))
    .map(({ alert }) => alert)
}

export function summarizeAlerts(alerts = []) {
  const summary = { total: 0, active: 0, suppressed: 0, critical: 0 }
  const receivers = new Set()
  const seen = new Set()
  for (const alert of alerts) {
    for (const receiver of alert.receivers || []) receivers.add(receiver)
    if (seen.has(alert.fingerprint)) continue
    seen.add(alert.fingerprint)
    summary.total++
    if (alert.state === 'active') summary.active++
    if (alert.state === 'suppressed') summary.suppressed++
    if (alert.labels?.severity === 'critical') summary.critical++
  }
  return { summary, receivers: [...receivers].sort() }
}

export function prepareAlertGroups(groups = []) {
  return groups.map(group => ({
    ...group,
    key: JSON.stringify([group.receiver, Object.entries(group.labels || {}).sort(([a], [b]) => a.localeCompare(b))]),
    alerts: sortAlerts(group.alerts)
  })).sort((a, b) => a.key.localeCompare(b.key))
}

export function clampAlertPage(page, total, pageSize) {
  return Math.max(1, Math.min(page, Math.ceil(total / pageSize) || 1))
}

export function pageAlertGroups(groups, page, pageSize) {
  let skip = (page - 1) * pageSize
  let remaining = pageSize
  const visible = []
  for (const group of groups) {
    if (remaining <= 0) break
    if (skip >= group.alerts.length) { skip -= group.alerts.length; continue }
    const alerts = group.alerts.slice(skip, skip + remaining)
    visible.push({ ...group, total: group.alerts.length, alerts })
    remaining -= alerts.length
    skip = 0
  }
  return visible
}
