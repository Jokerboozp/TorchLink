// Teaching protocol simulator for the selected demo gateways.
import net from 'node:net'
import dgram from 'node:dgram'
import {demoConfig} from '../lib/demo-config.mjs'
const {env,host,tcpPort,udpPort}=await demoConfig()
const duration=Number(env.IOT_DEMO_MINUTES || 120)*60000
const packet=(type,id,value)=>Buffer.from([0xaa,type,id,value,(0xaa+type+id+value)&255])
const sockets=new Set(), timers=new Set()
let stopping=false, sequence=0
function connect(id){
 if(stopping)return
 const socket=net.connect(tcpPort,host);sockets.add(socket);let pending=Buffer.alloc(0),interval
 socket.on('connect',()=>{socket.write(packet(1,id,25+id));interval=setInterval(()=>socket.write(packet(1,id,24+(sequence++%8))),10000);timers.add(interval)})
 socket.on('data',chunk=>{pending=Buffer.concat([pending,chunk]);while(pending.length>=5){const frame=pending.subarray(0,5);pending=pending.subarray(5);if(frame[1]===3)socket.write(packet(2,id,frame[3]))}})
 socket.on('error',()=>{})
 socket.on('close',()=>{sockets.delete(socket);clearInterval(interval);timers.delete(interval);if(!stopping){const timer=setTimeout(()=>{timers.delete(timer);connect(id)},3000);timers.add(timer)}})
}
connect(7);connect(8)
const udp=dgram.createSocket(host.includes(':')?'udp6':'udp4');udp.on('error',()=>{})
const sendUDP=()=>udp.send(packet(1,9,26+(sequence%4)),udpPort,host)
sendUDP();const udpTimer=setInterval(sendUDP,10000);timers.add(udpTimer)
function stop(){if(stopping)return;stopping=true;clearTimeout(endTimer);for(const timer of timers)clearTimeout(timer);for(const socket of sockets)socket.destroy();udp.close()}
const endTimer=setTimeout(stop,duration);process.on('SIGINT',stop);process.on('SIGTERM',stop)
console.log(`演示设备已启动：${host} TCP ${tcpPort}、UDP ${udpPort}；运行 ${duration/60000} 分钟后自动停止。`)
