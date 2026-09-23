// Teaching protocol simulator for the selected demo gateways.
import net from 'node:net' /* 引入当前代码需要的依赖。 */
import dgram from 'node:dgram' /* 引入当前代码需要的依赖。 */
import {demoConfig} from '../lib/demo-config.mjs' /* 引入当前代码需要的依赖。 */
const {env,host,tcpPort,udpPort}=await demoConfig() /* 执行当前语句并推进处理流程。 */
const duration=Number(env.IOT_DEMO_MINUTES || 120)*60000 /* 声明 duration。 */
const packet=(type,id,value)=>Buffer.from([0xaa,type,id,value,(0xaa+type+id+value)&255]) /* 声明 packet。 */
const sockets=new Set(), timers=new Set() /* 声明 sockets。 */
let stopping=false, sequence=0 /* 声明 stopping。 */
function connect(id){ /* 定义 connect 函数。 */
 if(stopping)return /* 判断条件并选择处理分支。 */
 const socket=net.connect(tcpPort,host);sockets.add(socket);let pending=Buffer.alloc(0),interval /* 声明 socket。 */
 socket.on('connect',()=>{socket.write(packet(1,id,25+id));interval=setInterval(()=>socket.write(packet(1,id,24+(sequence++%8))),10000);timers.add(interval)}) /* 执行当前语句并推进处理流程。 */
 socket.on('data',chunk=>{pending=Buffer.concat([pending,chunk]);while(pending.length>=5){const frame=pending.subarray(0,5);pending=pending.subarray(5);if(frame[1]===3)socket.write(packet(2,id,frame[3]))}}) /* 执行当前语句并推进处理流程。 */
 socket.on('error',()=>{}) /* 执行当前语句并推进处理流程。 */
 socket.on('close',()=>{sockets.delete(socket);clearInterval(interval);timers.delete(interval);if(!stopping){const timer=setTimeout(()=>{timers.delete(timer);connect(id)},3000);timers.add(timer)}}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
connect(7);connect(8) /* 执行当前语句并推进处理流程。 */
const udp=dgram.createSocket(host.includes(':')?'udp6':'udp4');udp.on('error',()=>{}) /* 声明 udp。 */
const sendUDP=()=>udp.send(packet(1,9,26+(sequence%4)),udpPort,host) /* 声明 sendUDP。 */
sendUDP();const udpTimer=setInterval(sendUDP,10000);timers.add(udpTimer) /* 执行当前语句并推进处理流程。 */
function stop(){if(stopping)return;stopping=true;clearTimeout(endTimer);for(const timer of timers)clearTimeout(timer);for(const socket of sockets)socket.destroy();udp.close()} /* 定义 stop 函数。 */
const endTimer=setTimeout(stop,duration);process.on('SIGINT',stop);process.on('SIGTERM',stop) /* 声明 endTimer。 */
console.log(`演示设备已启动：${host} TCP ${tcpPort}、UDP ${udpPort}；运行 ${duration/60000} 分钟后自动停止。`) /* 执行当前语句并推进处理流程。 */
