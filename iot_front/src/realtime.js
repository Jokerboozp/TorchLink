import mqtt from 'mqtt' /* 引入当前代码需要的依赖。 */
import { api, session } from './api' /* 引入当前代码需要的依赖。 */
import { permissionState, refreshPermissions, applyAccessVersion } from './permissions' /* 引入当前代码需要的依赖。 */

let client /* 声明 client。 */
let pollTimer /* 声明 pollTimer。 */
let brokerTimer /* 声明 brokerTimer。 */
let generation = 0 /* 声明 generation。 */

export function stopRealtime() { /* 执行当前语句并推进处理流程。 */
  generation++ /* 执行当前语句并推进处理流程。 */
  clearTimeout(pollTimer) /* 执行当前语句并推进处理流程。 */
  clearTimeout(brokerTimer) /* 执行当前语句并推进处理流程。 */
  client?.end(true) /* 执行当前语句并推进处理流程。 */
  client = undefined /* 更新 client 的值。 */
} /* 结束当前表达式或代码块。 */

export async function startRealtime(onMessage) { /* 执行当前语句并推进处理流程。 */
  stopRealtime() /* 执行当前语句并推进处理流程。 */
  const run = generation /* 声明 run。 */
  let previous = null /* 声明 previous。 */
  const brokerDelivered = new Map() /* 声明 brokerDelivered。 */
  const brokerMessage = (topic, payload) => { /* 声明 brokerMessage。 */
    const body = payload.toString() /* 声明 body。 */
    try { /* 执行当前语句并推进处理流程。 */
      const value = JSON.parse(body) /* 声明 value。 */
      const kind = topic.includes('/alarm/') ? 'alarm' : topic.includes('/device/state/') ? 'state' : '' /* 声明 kind。 */
      const id = kind === 'alarm' ? value.alarmId : kind === 'state' ? value.deviceId : '' /* 声明 id。 */
      if (id) { /* 判断条件并选择处理分支。 */
        const key = `${kind}:${id}` /* 声明 key。 */
        const normalized = JSON.stringify(value) /* 声明 normalized。 */
        const delivered = brokerDelivered.get(key) /* 声明 delivered。 */
        if (previous?.get(key) === normalized || delivered?.payload === normalized && delivered.expiresAt > Date.now()) return /* 判断条件并选择处理分支。 */
        brokerDelivered.set(key, { payload: normalized, expiresAt: Date.now() + 10000 }) /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
    } catch { /* Other MQTT topics do not use the event snapshot. */ } /* 结束当前表达式或代码块。 */
    onMessage?.(topic, body) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  // All accounts receive authoritative alarms over the authenticated API.
  // Broker availability and token renewal must not reset this snapshot.
  const poll = async () => { /* 声明 poll。 */
    try { /* 执行当前语句并推进处理流程。 */
      const data = await api('/api/v1/events') /* 声明 data。 */
      if (run !== generation) return /* 判断条件并选择处理分支。 */
      applyAccessVersion(data.accessVersion)
      permissionState.items = data.permissions || [] /* 更新 permissionState.items 的值。 */
      const next = new Map() /* 声明 next。 */
      for (const [kind, values] of [['alarm', data.alarms], ['state', data.devices]]) { /* 循环处理当前数据。 */
        for (const value of values || []) { /* 循环处理当前数据。 */
          const key = kind + ':' + (value.alarmId || value.deviceId) /* 声明 key。 */
          const payload = JSON.stringify(value) /* 声明 payload。 */
          next.set(key, payload) /* 执行当前语句并推进处理流程。 */
          if (previous && previous.get(key) !== payload) { /* 判断条件并选择处理分支。 */
            const delivered = brokerDelivered.get(key) /* 声明 delivered。 */
            if (delivered?.payload !== payload || delivered.expiresAt <= Date.now()) onMessage?.(`/iot/${kind === 'alarm' ? 'alarm/raised' : 'device/state'}/${session.tenant}`, payload) /* 判断条件并选择处理分支。 */
          } /* 结束当前表达式或代码块。 */
        } /* 结束当前表达式或代码块。 */
      } /* 结束当前表达式或代码块。 */
      previous = next /* 更新 previous 的值。 */
      for (const [key, delivered] of brokerDelivered) { /* 循环处理当前数据。 */
        if (delivered.expiresAt <= Date.now()) brokerDelivered.delete(key) /* 判断条件并选择处理分支。 */
      } /* 结束当前表达式或代码块。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      if (run !== generation) return /* 判断条件并选择处理分支。 */
      // Keep the last successful snapshot across transient network failures.
      if (error.status === 403) { await refreshPermissions().catch(() => {}); return } /* 判断条件并选择处理分支。 */
      if (error.status === 401) return /* 判断条件并选择处理分支。 */
    } /* 结束当前表达式或代码块。 */
    if (run === generation) pollTimer = setTimeout(poll, 3000) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  void poll() /* 执行当前语句并推进处理流程。 */
  if (session.role !== 'admin') return /* 判断条件并选择处理分支。 */

  // Administrators also retain parsed events and rule UI actions via MQTT.
  const connectBroker = async () => { /* 声明 connectBroker。 */
    try { /* 执行当前语句并推进处理流程。 */
      const auth = await api('/api/v1/mqtt/token', { method: 'POST' }) /* 声明 auth。 */
      if (run !== generation) return /* 判断条件并选择处理分支。 */
      client?.end(true) /* 执行当前语句并推进处理流程。 */
      // A client ID is only a connection identifier, not a credential.
      // randomUUID is unavailable on HTTP origins other than localhost.
      const id = globalThis.crypto?.randomUUID?.() || `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}` /* 声明 id。 */
      const connection = mqtt.connect(auth.websocketUrl, { /* 声明 connection。 */
        username: auth.username, /* 执行当前语句并推进处理流程。 */
        password: auth.token, /* 执行当前语句并推进处理流程。 */
        clientId: `iot-web-${id}`, /* 执行当前语句并推进处理流程。 */
        clean: true, /* 执行当前语句并推进处理流程。 */
        reconnectPeriod: 3000, /* 执行当前语句并推进处理流程。 */
        connectTimeout: 10000 /* 执行当前语句并推进处理流程。 */
      }) /* 结束当前表达式或代码块。 */
      client = connection /* 更新 client 的值。 */
      connection.on('connect', () => { /* 执行当前语句并推进处理流程。 */
        if (run === generation && client === connection) connection.subscribe(auth.subscriptions || [], { qos: 1 }) /* 判断条件并选择处理分支。 */
      }) /* 结束当前表达式或代码块。 */
      connection.on('message', (topic, payload) => { /* 执行当前语句并推进处理流程。 */
        if (run === generation && client === connection) brokerMessage(topic, payload) /* 判断条件并选择处理分支。 */
      }) /* 结束当前表达式或代码块。 */
      // Connection errors do not interrupt HTTP alarm delivery; MQTT retries itself.
      connection.on('error', () => {}) /* 执行当前语句并推进处理流程。 */
      brokerTimer = setTimeout(connectBroker, Math.max(60000, ((auth.expiresIn || 900) - 90) * 1000)) /* 更新 brokerTimer 的值。 */
    } catch { /* 结束当前表达式或代码块。 */
      if (run === generation) brokerTimer = setTimeout(connectBroker, 10000) /* 判断条件并选择处理分支。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
  await connectBroker() /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
