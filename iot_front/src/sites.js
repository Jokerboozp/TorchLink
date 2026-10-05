// 单位建筑页面的纯函数：组装树形结构、楼层名称与平面图坐标换算。
export function siteTree({ units = [], buildings = [], floors = [], points = [] } = {}) {
  const devices = new Map()
  for (const point of points) if (!point.componentId) devices.set(point.deviceId, point.unitId)
  const count = unitId => [...devices.values()].filter(id => id === unitId).length
  return units.map(unit => ({
    ...unit,
    deviceCount: count(unit.id),
    buildings: buildings
      .filter(item => item.unitId === unit.id)
      .map(building => ({
        ...building,
        floors: floors.filter(item => item.buildingId === building.id).sort((a, b) => b.level - a.level)
      }))
  }))
}

export const floorLabel = floor => (floor ? floor.name || `${floor.level} 层` : '')

// 点击或拖动位置换算为平面图宽高的比例，限制在图片范围内。
export function pointFraction(event, rect) {
  const clamp = value => Math.min(1, Math.max(0, value))
  const round = value => Math.round(value * 10000) / 10000
  return { x: round(clamp((event.clientX - rect.left) / rect.width)), y: round(clamp((event.clientY - rect.top) / rect.height)) }
}

// 列表附带的展示字段（设备名称）不属于保存接口，提交前去掉。
export function sitePayload(value) {
  const { deviceName, ...body } = value
  return body
}
