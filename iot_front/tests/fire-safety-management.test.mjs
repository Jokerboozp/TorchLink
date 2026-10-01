import test from 'node:test'
import assert from 'node:assert/strict'
import vm from 'node:vm'
import { computed, reactive, ref } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { managementPayload, inputTimestamp, localDateTimeInput, inspectionPayload, canReviewInspection, dispatchPayload, fireQuery } from '../src/fireSafetyManagement.js'

test('编辑请求保留版本，排除服务端提醒与审计字段', () => {
  const stored = { id:'asset',version:7,code:'EXT-1',stationId:'s',location:'门口',type:'dry_powder',specification:'4kg',manufacturer:'厂商',serialNumber:'n',manufacturedOn:'2026-01-01',serviceDueOn:'2027-01-01',retireOn:'2030-01-01',inspectionCycleDays:30,status:'active',notes:'',createdAt:123,reminders:[{kind:'inspection'}],lastInspectedAt:456 }
  const payload = managementPayload('extinguishers', reactive(stored))
  assert.equal(payload.version, 7)
  assert.equal(payload.serviceDueOn, '2027-01-01')
  for (const field of ['id','createdAt','reminders','lastInspectedAt']) assert.equal(Object.hasOwn(payload, field), false)
  assert.equal(Object.hasOwn(managementPayload('stations',{code:'new'}),'version'),false)
})

test('巡检每项必须明确选择结果，失败必须说明问题', () => {
  assert.throws(()=>inspectionPayload({checks:[{name:'外观',passed:null}],findings:''},3),/明确选择/)
  assert.throws(()=>inspectionPayload({checks:[{name:'外观',passed:false}],findings:' '},3),/发现的问题/)
  assert.throws(()=>inspectionPayload({checks:[],findings:''},3),/至少填写/)
  assert.throws(()=>inspectionPayload({checks:[{name:'外观',passed:true},{name:' 外观 ',passed:true}],findings:''},3),/重复/)
  assert.deepEqual(inspectionPayload({checks:[{name:' 外观 ',passed:false}],findings:' 筒体损伤 '},3),{version:3,checks:[{name:'外观',passed:false}],findings:'筒体损伤'})
})

test('整改复核取最新轮次并禁止提交人自复核', () => {
  const task={status:'reviewing',rectifications:[{submittedBy:'reviewer',status:'rejected'},{submittedBy:'worker',status:'pending'}]}
  assert.equal(canReviewInspection(task,'worker'),false)
  assert.equal(canReviewInspection(task,'reviewer'),true)
  assert.equal(canReviewInspection(task,''),false)
  assert.equal(canReviewInspection({...task,status:'completed'},'reviewer'),false)
  assert.equal(canReviewInspection({...task,rectifications:[]},'reviewer'),false)
})

test('多人出勤携带器材数量必须为正整数且不能重复', () => {
  const form={stationId:'station',title:'演练',type:'drill',location:'操场',personnelIds:['p1','p2'],equipment:[{equipmentId:'e1',quantity:2},{equipmentId:'e2',quantity:1}],startedAtInput:'2026-10-01T09:00'}
  const value=dispatchPayload(form)
  assert.deepEqual(value.personnelIds,['p1','p2'])
  assert.equal(value.startedAt,new Date(2026,9,1,9).getTime())
  value.personnelIds.push('p3');assert.equal(form.personnelIds.length,2)
  assert.throws(()=>dispatchPayload({...form,equipment:[{equipmentId:'e1',quantity:1.5}]}),/正整数/)
  assert.throws(()=>dispatchPayload({...form,equipment:[{equipmentId:'e1',quantity:1},{equipmentId:'e1',quantity:1}]}),/重复/)
})

test('日期时间保持本地时区，分页筛选保留0且排除空值', () => {
  const date=new Date(2026,9,1,9,30)
  assert.equal(localDateTimeInput(date.getTime()),'2026-10-01T09:30')
  assert.equal(inputTimestamp(localDateTimeInput(date.getTime())),date.getTime())
  assert.throws(()=>inputTimestamp(''),/有效/)
  const query=new URLSearchParams(fireQuery({q:'名称 / A',status:'',remindDays:0},{page:2,pageSize:20}))
  assert.equal(query.get('q'),'名称 / A');assert.equal(query.has('status'),false);assert.equal(query.get('remindDays'),'0');assert.equal(query.get('page'),'2')
})

function stationPage(api = async()=>({items:[],total:0}), allowed = true) {
  const source=setupScript(new URL('../src/views/FireStationsView.vue',import.meta.url))
  const context=vm.createContext({computed,reactive,ref,watch(){},onMounted(){},defineEmits(){},can:()=>allowed,api,notifyError(){},UiMessage:{success(){}},UiMessageBox:{},errorMessage:error=>error.message,managementPayload,localDateTimeInput,inputTimestamp,dispatchPayload,fireQuery})
  vm.runInContext(source+'\nglobalThis.subject={open,form,dialog,tab,load,loadError,loading,save}',context)
  return context.subject
}

test('真实Vue响应式行可打开编辑，未提交的修改不污染表格', () => {
  const row=reactive({id:'station',version:4,code:'S1',name:'原名称',type:'micro',notes:'原备注',enabled:true})
  const page=stationPage()
  assert.doesNotThrow(()=>page.open('stations',row))
  assert.equal(page.dialog.value,true);assert.equal(page.form.version,4)
  page.form.name='未提交名称';assert.equal(row.name,'原名称')
})

test('统计标签不发出不存在的列表请求',async()=>{
  const requests=[],page=stationPage(async path=>{requests.push(path);return {}})
  page.tab.value='statistics';await page.load()
  assert.deepEqual(requests,[]);assert.equal(page.loadError.value,'');assert.equal(page.loading.value,false)
})

test('保存失败保留编辑内容和版本供用户核对',async()=>{
  const row=reactive({id:'station',version:4,code:'S1',name:'原名称',type:'micro',notes:'原备注',enabled:true})
  const requests=[],page=stationPage(async(path,options)=>{if(options?.method==='PUT'){requests.push(JSON.parse(options.body));throw Error('记录已更新，请刷新后重试')}return {}})
  page.open('stations',row);page.form.name='新名称';await page.save()
  assert.equal(requests[0].version,4);assert.equal(requests[0].name,'新名称');assert.equal(page.form.name,'新名称');assert.equal(page.dialog.value,true)
})

test('没有编辑权限时不发起写入请求',async()=>{
  const writes=[],page=stationPage(async(path,options)=>{if(options?.method)writes.push(path);return {}},false)
  page.open('stations',{id:'s',version:1,name:'消防站',code:'s'});await page.save();assert.deepEqual(writes,[])
})

test('巡检状态筛选不被错误用于灭火器资产统计',async()=>{
  const requests=[]
  const source=setupScript(new URL('../src/views/ExtinguishersView.vue',import.meta.url))
  const context=vm.createContext({computed,reactive,ref,watch(){},onMounted(){},defineEmits(){},api:async path=>{requests.push(path);return {}},fireQuery,extinguisherTypes:[],inspectionStates:[]})
  vm.runInContext(source+'\nglobalThis.subject={tab,filters,loadStatistics}',context)
  const page=context.subject
  page.tab.value='inspections';Object.assign(page.filters,{stationId:'station',status:'rectifying',q:'问题'})
  await page.loadStatistics()
  const query=new URL(requests[0],'http://localhost').searchParams
  assert.equal(query.get('stationId'),'station');assert.equal(query.has('status'),false);assert.equal(query.has('q'),false)
  page.tab.value='assets';page.filters.status='active';await page.loadStatistics()
  assert.equal(new URL(requests[1],'http://localhost').searchParams.get('status'),'active')
})
