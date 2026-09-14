// Read-only loopback report server. Explicit allowlist excludes logs and credentials.
import {createServer} from 'node:http'
import {readFile} from 'node:fs/promises'
const dir=new URL('../../.e2e/demo-20260914/',import.meta.url)
const files=new Set(['测试报告.html','演示点表.xlsx','演示点表.csv','演示报文.json','演示TCP协议.go','演示消防处置手册.txt','智能助手回答.txt','inspection.json'])
createServer(async(req,res)=>{try{const path=decodeURIComponent(new URL(req.url,'http://localhost').pathname).slice(1)||'测试报告.html';if(req.method!=='GET'||(!files.has(path)&&!/^screenshots\/\d{2}\.png$/.test(path))){res.writeHead(404);return res.end()}const type=path.endsWith('.html')?'text/html; charset=utf-8':path.endsWith('.png')?'image/png':path.endsWith('.xlsx')?'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet':'text/plain; charset=utf-8';const content=await readFile(new URL(path,dir));res.writeHead(200,{'Content-Type':type,'X-Content-Type-Options':'nosniff'});res.end(content)}catch{res.writeHead(404);res.end()}}).listen(4173,'127.0.0.1',()=>console.log('演示报告：http://127.0.0.1:4173'))
