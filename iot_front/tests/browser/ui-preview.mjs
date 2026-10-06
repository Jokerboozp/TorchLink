import http from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
// 独立的界面验收夹具：仅绑定回环地址，不代理真实服务。
const root = fileURLToPath(new URL('../../dist/', import.meta.url))
const port = Number(process.env.IOT_UI_PREVIEW_PORT || 4173)
const now = Number(process.env.IOT_UI_PREVIEW_NOW) || Date.now() // 固定时间便于截图对比。
const products = [
  {
    id: 'product-demo',
    name: '烟雾探测器',
    category: 'smoke',
    transport: 'MQTT',
    payloadFormat: 'json',
    status: 'ENABLED',
    description: '浏览器验收示例产品'
  }
]
const devices = [
  {
    device: {
      id: 'device-demo',
      name: '一层走廊烟感',
      productId: 'product-demo',
      deviceRole: 'DIRECT',
      status: 'ENABLED',
      accessKey: '示例接入标识',
      secretHint: '示例'
    },
    runtimeState: { businessStatus: 'ONLINE', lastSeenAt: now, connectionStatus: 'CONNECTED' },
    childCount: 0
  }
]
const alarms = [
  {
    alarmId: 'alarm-demo',
    deviceId: 'device-demo',
    deviceName: '一层走廊烟感',
    alarmType: 'FIRE_RISK',
    alarmLevel: 'HIGH',
    status: 'ACTIVE',
    source: 'device',
    triggerCount: 1,
    firstTriggeredAt: now,
    lastTriggeredAt: now
  }
]
// 排班页面的消防站、人员、班次与排班样例，供弹窗与日历截图对比。
const dayStart = new Date(now).setHours(8, 0, 0, 0) + 86400e3 // 次日白班，可以申请换班。
const fireOptions = {
  stations: [{ id: 'station-demo', name: '城东消防站', enabled: true }],
  personnel: [
    { id: 'person-a', name: '张伟', stationId: 'station-demo', enabled: true },
    { id: 'person-b', name: '李娜', stationId: 'station-demo', enabled: true },
    { id: 'person-c', name: '王强', stationId: 'station-demo', enabled: true }
  ],
  shifts: [
    { id: 'shift-day', version: 1, name: '白班', startTime: '08:00', endTime: '17:00' },
    { id: 'shift-night', version: 1, name: '夜班', startTime: '20:00', endTime: '08:00' }
  ]
}
const dutyAssignment = {
  id: 'assignment-demo',
  version: 1,
  stationId: 'station-demo',
  shiftId: 'shift-day',
  personnelIds: ['person-a', 'person-b'],
  startAt: dayStart,
  endAt: dayStart + 9 * 3600e3,
  notes: '节前加强值守'
}
const dutySwap = {
  id: 'swap-demo',
  version: 1,
  assignmentId: dutyAssignment.id,
  assignment: dutyAssignment,
  fromPersonnelId: 'person-b',
  toPersonnelId: 'person-c',
  reason: '家中有事',
  status: 'pending',
  requestedBy: 'zhangwei',
  createdAt: now
}
// 灭火器台账与三种处理阶段的巡检任务。
const extinguisher = {
  id: 'ext-demo',
  version: 1,
  code: 'MHQ-001',
  stationId: 'station-demo',
  location: '一号楼一层东侧',
  type: 'dry_powder',
  specification: '4 kg',
  manufacturer: '示例厂家',
  serialNumber: 'SN-001',
  manufacturedOn: '2024-03-01',
  serviceDueOn: '2026-10-01',
  retireOn: '2030-03-01',
  inspectionCycleDays: 30,
  status: 'active',
  notes: '',
  lastInspectedAt: now - 20 * 86400e3,
  nextInspectionOn: '2026-10-01',
  reminders: [{ kind: 'service', dueOn: '2026-10-01', status: 'soon' }]
}
const inspection = (id, status, extra = {}) => ({
  id,
  version: 1,
  extinguisherId: extinguisher.id,
  assigneeId: 'person-a',
  dueAt: now + 86400e3,
  status,
  notes: '月度巡检',
  createdBy: 'admin',
  ...extra
})
const inspections = [
  inspection('inspection-pending', 'pending'),
  inspection('inspection-rectifying', 'rectifying', { result: 'fail', findings: '压力表指针偏低' }),
  inspection('inspection-reviewing', 'reviewing', {
    result: 'fail',
    findings: '喷管老化',
    rectifications: [{ action: '已更换喷管', submittedBy: 'zhangwei', submittedAt: now, status: 'pending' }]
  })
]
fireOptions.extinguishers = [extinguisher]
fireOptions.inspectionChecks = ['外观完好', '压力正常', '喷管完好']
const knowledgeDocument = {
  id: 'document-demo',
  filename: '消防设备手册',
  workflowId: 'assistant',
  category: 'manual',
  tags: ['烟感'],
  status: 'INDEXED',
  createdAt: now,
  metadata: { chunks: 3, size: 2048 }
}
// 设备详情：带子设备、命令、健康信号与在线会话的 MQTT 网关，覆盖详情抽屉的各个分区。
const connection = {
  device: { id: 'device-demo', name: '一层走廊烟感', productId: 'product-demo', deviceRole: 'GATEWAY', createdAt: now },
  product: {
    name: '烟雾探测器',
    thingModel: {
      commands: [
        {
          identifier: 'mute',
          name: '消音',
          fields: [{ identifier: 'duration', name: '时长', dataType: 'integer', unit: '秒', required: true }]
        }
      ]
    }
  },
  connector: 'MQTT',
  accessInfo: {
    kind: 'mqtt',
    mqttBroker: 'mqtt://127.0.0.1:1883',
    clientId: 'device-demo',
    username: '示例接入标识',
    upTopic: 'devices/device-demo/up',
    downTopic: 'devices/device-demo/down',
    tokenEndpoint: '/api/v1/device-auth/mqtt-token'
  },
  credentialEnabled: true,
  mqttCommandAvailable: true,
  connection: { businessStatus: 'ONLINE', connectionStatus: 'CONNECTED', dataStatus: 'NORMAL', lastSeenAt: now, lastConnectAt: now },
  ingest: { rawReceived: true, parsed: true, rawMessageId: 'raw-demo' },
  protocolId: 'json',
  protocolVersion: '1',
  profile: { id: 'profile-demo', enabled: true, runtimeStatus: 'RUNNING', childProducts: [{ type: 'smoke', productId: 'product-demo' }] },
  sessions: [{ profileId: 'profile-demo', remoteAddress: '10.0.0.8:50211', protocolId: 'json', protocolVersion: '1', lastSeenAt: now }],
  latestProperties: [{ timestamp: now, properties: { temperature: 25, smoke: false } }],
  latest: { messageId: 'msg_raw-demo', messageType: 'PROPERTY', timestamp: now },
  recentAlarms: alarms,
  revocations: [{ id: 'revocation-demo', status: 'REVOKED' }]
}
const connectionLists = {
  history: [{ recordedAt: now, state: { connectionStatus: 'CONNECTED', businessStatus: 'ONLINE', statusSource: 'MESSAGE' } }],
  events: [{ timestamp: now, event: { type: 'heartbeat' } }],
  commands: [{ id: 'command-demo', type: 'mute', status: 'DELIVERED', reply: { ok: true } }],
  children: [
    {
      device: { id: 'child-demo', name: '二层烟感', childAddress: '2' },
      productName: '烟雾探测器',
      binding: { protocolId: 'json', version: '1' },
      runtimeState: { lastSeenAt: now, businessStatus: 'ONLINE' }
    }
  ]
}
const list = items => ({ items, total: items.length, count: items.length, page: 1, pageSize: 20 })
const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, 'http://localhost')
  if (u.pathname.startsWith('/api/')) {
    res.setHeader('Content-Type', 'application/json; charset=utf-8')
    let data = list([])
    if (u.pathname === '/api/v1/auth/login')
      data = { accessToken: 'local-ui-fixture', tenantId: '界面验收租户', role: 'admin', permissions: ['*'], platformVersion: 'v1.2.3' }
    else if (u.pathname === '/api/v1/auth/me')
      data = { tenantId: '界面验收租户', username: 'admin', role: 'admin', permissions: ['*'], platformVersion: 'v1.2.3' }
    else if (u.pathname === '/api/v1/events') data = { permissions: ['*'], alarms: [], devices: [] }
    else if (u.pathname === '/api/v1/ops/capacity/status') data = { enabled: false }
    else if (u.pathname === '/api/v1/test-devices/provision' && req.method === 'POST')
      data = {
        device: {
          id: 'device-test-preview',
          name: '界面验收测试设备',
          productId: 'product-demo',
          accessKey: '示例接入标识',
          status: 'ENABLED'
        },
        product: products[0],
        protocolPackage: { id: 'protocol-test-preview', parserType: 'JSON', version: '1' },
        templates: {
          data: { messageId: '<unique>', properties: { temperature: 25 } },
          alarm: { messageId: '<unique>', properties: { temperature: 85, smoke: true } },
          recovery: { messageId: '<unique>', properties: { temperature: 25, smoke: false } },
          event: { messageId: '<unique>', event: { type: 'heartbeat' } }
        }
      } /* 仅返回合成数据，不写入业务服务。 */
    else if (req.method === 'POST' && u.pathname.endsWith('/knowledge-binding/test'))
      data = {
        items: [
          {
            documentId: 'document-demo',
            filename: '消防设备手册',
            chunkIndex: 1,
            score: 0.812,
            content: '烟感持续报警时，先查看现场是否有烟雾或明火，再核对设备状态。'
          }
        ],
        keywordOnly: false,
        durationMs: 42
      }
    else if (req.method !== 'GET') {
      res.statusCode = 503
      data = { message: '界面验收环境不执行实际业务操作' }
    } else if (u.pathname === '/api/v1/products') data = list(products)
    else if (u.pathname === '/api/v1/device-registry') data = list(devices)
    else if (u.pathname === '/api/v1/devices')
      data = { ...list([]), total: u.searchParams.has('unregistered') ? 0 : 1, online: 1, offline: 0 }
    else if (u.pathname === '/api/v1/alarms') data = list(alarms)
    else if (u.pathname === '/api/v1/alarms/alarm-demo') data = alarms[0]
    else if (u.pathname === '/api/v1/device-registry/device-demo/connection') data = connection
    else if (u.pathname === '/api/v1/device-registry/device-demo/signals')
      data = { items: [{ signalType: 'STUCK_VALUE', property: 'temperature', strength: 0.6, windowEnd: now }] }
    else if (u.pathname === '/api/v1/device-registry/device-demo/history')
      data = list(connectionLists[u.searchParams.get('kind') === 'event' ? 'events' : 'history'])
    else if (u.pathname === '/api/v1/device-registry/device-demo/commands') data = list(connectionLists.commands)
    else if (u.pathname === '/api/v1/device-registry/device-demo/children') data = list(connectionLists.children)
    else if (u.pathname === '/api/v1/fire-safety/options') data = fireOptions
    else if (u.pathname === '/api/v1/duty/assignments') data = list([dutyAssignment])
    else if (u.pathname === '/api/v1/duty/shifts') data = list(fireOptions.shifts)
    else if (u.pathname === '/api/v1/duty/swaps') data = list([dutySwap])
    else if (u.pathname === '/api/v1/extinguishers') data = list([extinguisher])
    else if (u.pathname === '/api/v1/extinguishers/statistics')
      data = {
        total: 1,
        active: 1,
        maintenance: 0,
        retired: 0,
        overdue: 0,
        soon: 1,
        pendingInspections: 1,
        overdueInspections: 0,
        rectifying: 1,
        reviewing: 1
      }
    else if (u.pathname === `/api/v1/extinguishers/${extinguisher.id}`) data = extinguisher
    else if (u.pathname === '/api/v1/extinguisher-inspections') data = list(inspections)
    else if (u.pathname.startsWith('/api/v1/extinguisher-inspections/'))
      data = inspections.find(item => item.id === u.pathname.split('/').pop()) || inspections[0]
    else if (u.pathname === '/api/v1/ai/providers')
      data = {
        ...list([]),
        active: { id: 'disabled', name: '未启用', enabled: false },
        healthy: false,
        healthMessage: '验收环境未连接模型',
        config: null
      }
    else if (u.pathname === '/api/v1/ai/embedding-config')
      data = {
        baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
        model: 'text-embedding-v4',
        apiKeyConfigured: false,
        dimensions: 1024,
        batchSize: 10,
        queryInstruction: '',
        timeoutSeconds: 60
      }
    else if (u.pathname === '/api/v1/knowledge/documents')
      data = {
        ...list([knowledgeDocument]),
        persistentIndex: true,
        summary: { documents: 1, indexed: 1, failed: 0, chunks: 3, bytes: 2048 },
        indexMode: 'postgres-pgvector',
        embeddingModel: 'text-embedding-v4'
      }
    else if (u.pathname === `/api/v1/knowledge/documents/${knowledgeDocument.id}`)
      data = {
        document: knowledgeDocument,
        index: {
          mode: 'postgres-pgvector',
          vectorizer: 'embedding-api',
          chunking: { strategy: 'fixed-window-overlap', size: 800, overlap: 100 },
          extractedChars: 2048,
          chunkCount: 2,
          embeddingModel: 'text-embedding-v4'
        },
        chunks: [
          {
            chunkId: 'chunk-1',
            startChar: 0,
            endChar: 800,
            overlapChars: 0,
            characterCount: 800,
            vectorized: true,
            content: '烟感持续报警时，先查看现场是否有烟雾或明火，再核对设备状态。'
          },
          {
            chunkId: 'chunk-2',
            startChar: 700,
            endChar: 1500,
            overlapChars: 100,
            characterCount: 800,
            vectorized: true,
            content: '确认误报后清洁探测器并记录处置过程。'
          }
        ]
      }
    else if (u.pathname === '/api/v1/ai/workflows')
      data = list([
        { id: 'assistant', name: '运维助手', description: '查询设备和告警，检索处置知识', enabled: true, capabilities: ['chat'] }
      ])
    else if (u.pathname === '/api/v1/ai/conversations')
      data = list([
        { id: 'conv-1', workflowId: 'assistant', title: '一号楼烟感告警原因', messageCount: 2, createdAt: now, updatedAt: now },
        { id: 'conv-2', workflowId: 'assistant', title: '本周离线设备汇总', messageCount: 4, createdAt: now, updatedAt: now }
      ])
    else if (u.pathname.startsWith('/api/v1/ai/conversations/'))
      data = {
        id: u.pathname.split('/').pop(),
        title: '一号楼烟感告警原因',
        messages: [
          { seq: 1, role: 'user', text: '一号楼烟感为什么告警？', status: 'SUCCEEDED', createdAt: now },
          { seq: 2, role: 'assistant', text: '3 楼烟感浓度超过阈值，建议现场确认。', status: 'SUCCEEDED', createdAt: now }
        ]
      }
    else if (u.pathname.endsWith('/knowledge-binding'))
      data = { retrievalMode: 'always', topK: 5, minScore: 0.25, noMatchPolicy: 'allow-model' }
    else if (u.pathname === '/api/v1/ai/health-inspection/progress') data = { status: 'idle' }
    else if (u.pathname === '/api/v1/connectors/types')
      data = list(['MQTT', 'HTTP', 'TCP', 'UDP', 'MODBUS_TCP'].map(type => ({ type, name: type, supported: true })))
    res.end(JSON.stringify(data))
    return
  }
  try {
    // 与 nginx.conf 一致：无扩展名的页面地址回退到 index.html，支持刷新深链接。
    const pathname = u.pathname === '/' || !extname(u.pathname) ? 'index.html' : u.pathname
    const content = await readFile(join(root, pathname))
    res.setHeader(
      'Content-Type',
      { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[extname(pathname)] ||
        'application/octet-stream'
    )
    res.end(content)
  } catch {
    res.statusCode = 404
    res.end('未找到文件')
  }
})
server.listen(port, '127.0.0.1', () => console.log(`界面验收预览：http://127.0.0.1:${port}（仅合成数据，所有业务写操作禁用）`))
