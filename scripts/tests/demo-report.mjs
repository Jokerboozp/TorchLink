import {readFile,writeFile,mkdir} from 'node:fs/promises'
const dir='.e2e/demo-20260914'
const state=JSON.parse(await readFile(`${dir}/results.json`,'utf8'))
const browser=JSON.parse(await readFile(`${dir}/browser-results.json`,'utf8'))
const inspection=JSON.parse(await readFile(`${dir}/inspection.json`,'utf8'))
const descriptions={
 '运行总览':'查看设备在线分布、告警统计和数据趋势；TCP/UDP、MQTT 演示设备每10秒持续上报。',
 '协议管理':'已发布 JSON 字段映射、Excel 点表和 Go TCP/UDP 协议。Excel 样例实际解析为温度25.5℃、湿度48%。点击“协议生成”上传示例，字段以输入框对照编辑。',
 '产品管理':'消防环境监测、TCP 消防主机、MQTT 可控设备三个演示产品。TCP 产品绑定已编译的 Go 协议；MQTT 产品定义“设置目标温度”命令。',
 '设备管理':'演示设备包含 TCP 两台、UDP一台、MQTT温控器、HTTP温湿度传感器，以及实体网关和子烟感。后两者演示归属关系，未模拟它们的上报。',
 '接入网关':'同一 TCP 消防主机产品对应两个接入网关：TCP 29075、UDP 29076。可查看实际连接会话。',
 '接入测试':'已发送内置正常数据、告警、恢复、事件四种模板，均真实解析成功。原始命令调试统一在这里进行。',
 '摄像头映射':'已登记“演示 · 一层走廊摄像头”并关联温湿度传感器，位置为演示楼1F。仅验证元数据和映射，未接入真实视频。',
 '告警中心':'温湿度演示设备保留58℃触发后确认并关闭的告警，以及62℃新触发的活动告警。',
 '智能巡检':`真实模型巡检成功。报告快照：${inspection.report.summary}；后续新建的设备不在这份早期快照内，可在平台再次巡检。`,
 '原始报文':'HTTP、TCP、UDP、MQTT上报及命令应答均有实际归档。已验证详情、下载和11条 DRY_RUN 回放。',
 '告警规则':'“演示 · 高温告警”限定 demoGroup=demo-20260914 且温度>40℃，不会匹配普通设备。',
 '知识库':'“演示消防处置手册.txt”实际上传，Weaviate索引状态INDEXED；真实模型问答已调用知识检索。',
 '模型管理':'沿用当前模型完成连接测试；未修改全局模型配置。',
 '智能助手':'真实 SSE 问答完成，查询演示设备、告警和手册，输出处置建议。截图复现真实回答缓存，未覆盖你现有浏览器的聊天记录。',
 '备份中心':`已生成 FULL 备份 ${state.ids.backup}，完成文件清单读取和文件校验。未执行业务数据库恢复。`,
 '设备控制表单与真实回执':'在真实页面选择“设置目标温度”，输入28℃并执行；模拟温控器应答成功，温度随即更新。点击连接详情顶部“刷新”可更新异步回执。'
}
const esc=s=>String(s).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;')
const downloads=['演示点表.xlsx','演示点表.csv','演示报文.json','演示TCP协议.go','演示消防处置手册.txt','智能助手回答.txt','inspection.json']
const intro='2026-09-14 · 当前本机平台验收。演示资源统一使用“演示”名称或 demo-20260914 标识。全量 Go 测试、协议包 Go 测试、前端74项测试和前端构建通过。'
const notes='本报告覆盖15个菜单及主要业务链路，不代表每个按钮和所有异常组合均已验收。真实物理设备、真实摄像头视频、现场网络与生产数据库恢复未验证。智能助手和巡检截图使用本次真实接口输出的本地缓存；其余截图读取当前平台。首次脚本错误（字段起始地址、CHILD枚举、succeeded状态）已修正；初始巡检404表示没有任务。Windows队列故障测试改为注入不可读条目，避免重命名被锁定目录；复测通过。本机Vite过期依赖缓存引发白屏，强制重建后全菜单复测通过。'
const cards=browser.results.map(r=>`<section id="p${browser.results.indexOf(r)}"><h2>${esc(r.name)} <small>${esc(r.status)}</small></h2><p>${esc(descriptions[r.name]||r.detail||'')}</p>${r.file?`<a href="${r.file}" target="_blank"><img loading="lazy" src="${r.file}" alt="${esc(r.name)}实际页面"></a>`:''}</section>`).join('')
await writeFile(`${dir}/测试报告.html`,`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>IoT 平台功能演示与测试报告</title><style>body{margin:0;background:#f2f5fa;color:#182b45;font:16px/1.7 system-ui}main{max-width:1120px;margin:auto;padding:40px 24px}h1{font-size:32px}h2{font-size:23px}a{color:#0967d4}nav{display:flex;gap:12px;flex-wrap:wrap}nav a,.button{background:white;padding:8px 15px;border-radius:8px}section{background:white;border:1px solid #dce3ed;border-radius:14px;padding:24px;margin:24px 0}img{width:100%;border:1px solid #eee;border-radius:6px}small{color:#078546;font-size:15px}details{padding:18px;background:#fff}pre{white-space:pre-wrap;overflow-wrap:anywhere}</style><main><h1>IoT 平台 · 功能演示与测试报告</h1><p>${intro}</p><p><a class="button" href="http://localhost:5173" target="_blank">打开当前本机平台</a></p><p>推荐体验：设备管理 → 演示 MQTT 温控器 → 连接详情 → 设备控制 → 设置目标温度。TCP/UDP与MQTT模拟器各运行两小时，结束后仍可查看历史数据。</p><nav>${browser.results.map((r,i)=>`<a href="#p${i}">${esc(r.name)}</a>`).join('')}</nav><h2>可直接使用的示例文件</h2><nav>${downloads.map(f=>`<a download href="${encodeURIComponent(f)}">${esc(f)}</a>`).join('')}</nav>${cards}<details><summary>测试范围与修复记录</summary><p>${notes}</p><p>备份文件和系统模型配置使用平台生成标识，不改写为“演示”。</p></details></main></html>`)
await mkdir('docs/testing',{recursive:true})
await writeFile('docs/testing/2026-09-14-功能验收.md',`# 本机功能演示验收（2026-09-14）\n\n${intro}\n\n结果图册：[测试报告](../../.e2e/demo-20260914/测试报告.html)。本机平台：http://localhost:5173 。\n\n| 功能 | 结果 | 实测内容 |\n|---|---|---|\n${browser.results.map(r=>`| ${r.name} | ${r.status} | ${descriptions[r.name]||r.detail||''} |`).join('\n')}\n\n## 范围与修复\n\n${notes}\n\n## 复现\n\n从仓库根目录执行，沿用私有 .env.local 登录，不打印凭据：\n\n- scripts/tests/demo-platform.mjs：阶段 inspect / business / protocols / services / refine / children / mqtt（有真实演示数据写入；mqtt会轮换演示设备凭据，勿与持续模拟器同时运行）。\n- scripts/tests/demo-devices.mjs：TCP/UDP本机模拟器，两小时自动停止。\n- scripts/tests/demo-mqtt-device.mjs：MQTT本机模拟器，两小时自动停止，只操作固定演示设备。\n- iot_front/tests/browser/demo-platform-check.mjs：真实页面检查，并对演示温控器发送28℃命令；需先启动模拟器。\n- scripts/tests/demo-report.mjs：根据实测记录生成图册与本文件。\n\n原始尝试记录保存在 .e2e/demo-20260914/results.json，保留已纠正的测试脚本失败用于追溯；不要将早期尝试数量当作当前缺陷数量。\n`)
console.log('已生成测试报告与验收文档')
