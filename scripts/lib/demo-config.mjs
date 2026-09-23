import { readFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { parseEnv } from 'node:util' /* 引入当前代码需要的依赖。 */
import { resolve } from 'node:path' /* 引入当前代码需要的依赖。 */

export async function demoConfig(input = process.env) { /* 执行当前语句并推进处理流程。 */
  let file = {} /* 声明 file。 */
  try { file = parseEnv(await readFile(input.IOT_DEMO_ENV_FILE || '.env.local', 'utf8')) } /* 执行当前语句并推进处理流程。 */
  catch (error) { if (input.IOT_DEMO_ENV_FILE || error.code !== 'ENOENT') throw Error('无法读取演示配置文件') } /* 执行当前语句并推进处理流程。 */
  const env = { ...file, ...input } /* 声明 env。 */
  let address /* 声明 address。 */
  try { address = new URL(env.IOT_DEMO_ORIGIN || 'http://127.0.0.1:5173') } catch { throw Error('平台地址格式无效') } /* 执行当前语句并推进处理流程。 */
  if (!['http:', 'https:'].includes(address.protocol) || address.username || address.password || address.search || address.hash || address.pathname !== '/') throw Error('平台地址须为不含凭据和路径的 HTTP/HTTPS 根地址') /* 判断条件并选择处理分支。 */
  if(env.IOT_DEMO_MQTT_URL) { /* 判断条件并选择处理分支。 */
    let broker /* 声明 broker。 */
    try {broker=new URL(env.IOT_DEMO_MQTT_URL)}catch{throw Error('MQTT 地址格式无效')} /* 执行当前语句并推进处理流程。 */
    if(!['mqtt:','mqtts:','ws:','wss:'].includes(broker.protocol) || broker.username || broker.password || broker.search || broker.hash)throw Error('MQTT 地址须使用 mqtt/mqtts/ws/wss，不能内嵌凭据或查询参数') /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  const prefix = env.IOT_DEMO_PREFIX || 'demo-20260914' /* 声明 prefix。 */
  if (!/^demo-[a-z0-9-]{1,32}$/.test(prefix)) throw Error('演示前缀须为 demo- 开头、总长不超过37位的小写字母、数字和横线') /* 判断条件并选择处理分支。 */
  const tenant = env.IOT_DEMO_TENANT || (env.IOT_ADMIN_TENANTS || 'tenant_001').split(',')[0].trim() /* 声明 tenant。 */
  if (!/^[A-Za-z0-9_.-]+$/.test(tenant)) throw Error('租户标识含不支持的字符') /* 判断条件并选择处理分支。 */
  const port = (value, fallback) => { const n=Number(value || fallback); if(!Number.isInteger(n) || n<1 || n>65535) throw Error('演示端口须为1至65535'); return n } /* 声明 port。 */
  return { env, origin:address.origin, prefix, tenant, dir:resolve(env.IOT_DEMO_OUTPUT || `.e2e/${prefix}`), /* 返回当前处理结果。 */
    host:env.IOT_DEMO_DEVICE_HOST || address.hostname.replace(/^\[|\]$/g,''), /* 执行当前语句并推进处理流程。 */
    tcpPort:port(env.IOT_DEMO_TCP_PORT,29075), udpPort:port(env.IOT_DEMO_UDP_PORT,29076) } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

export function redactDemo(value, env) { /* 执行当前语句并推进处理流程。 */
  let text=String(value) /* 声明 text。 */
  for(const [key,secret] of Object.entries(env)) if(/PASSWORD|SECRET|TOKEN|API_KEY/i.test(key) && secret && String(secret).length>=4) text=text.replaceAll(String(secret),'<REDACTED>') /* 循环处理当前数据。 */
  return text /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function demoGatewaysReady(profiles, config, ids) { /* 执行当前语句并推进处理流程。 */
  if(!ids.tcpProduct || !ids.tcpProtocol?.id)return false /* 判断条件并选择处理分支。 */
  return [['tcp',config.tcpPort],['udp',config.udpPort]].every(([network,port])=>profiles.some(p=> /* 返回当前处理结果。 */
    p.id===`${config.prefix}-${network}-gateway` && p.productId===ids.tcpProduct && /* 执行当前语句并推进处理流程。 */
    p.protocolId===ids.tcpProtocol.id && p.protocolVersion===ids.tcpProtocol.version && /* 执行当前语句并推进处理流程。 */
    p.mode==='listener' && p.connectionMode!=='dial' && p.network===network && Number(p.port)===port && p.enabled && p.runtimeStatus==='LISTENING')) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
