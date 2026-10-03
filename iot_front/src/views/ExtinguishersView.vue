<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { api, notifyError, session } from '../api'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { errorMessage } from '../presentation'
import { can } from '../permissions'
import { extinguisherTypes, inspectionStates, statusLabel, statusTone, dateTimeLabel, dateLabel } from '../fireSafety'
import { managementPayload, localDateTimeInput, inputTimestamp, inspectionPayload, canReviewInspection, fireQuery } from '../fireSafetyManagement'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
defineEmits(['navigate'])

const tab = ref('assets'), rows = ref([]), page = ref(1), pageSize = ref(20), total = ref(0), loading = ref(false), loadError = ref('')
const filters = reactive({ q:'', stationId:'', status:'', due:'', remindDays:30 })
const options = reactive({stations:[],personnel:[],extinguishers:[],inspectionChecks:[]}), optionsError = ref('')
const statistics = ref(null), statisticsError = ref('')
const dialog = ref(false), saving = ref(false), saveError = ref(''), form = reactive({})
const assetDetail = ref(null), taskDetail = ref(null), opening = ref(false), deleting = ref(false)
const taskDialog = ref(false), taskForm = reactive({extinguisherId:'',assigneeId:'',dueAtInput:'',notes:''}), taskStationId = ref(''), taskSaving = ref(false), taskError = ref('')
const action = ref(''), actionTask = ref(null), actionForm = reactive({checks:[],findings:'',action:'',approved:true,note:'',reason:''}), actionSaving = ref(false), actionError = ref('')
const assetStates = [{value:'active',label:'在用'},{value:'maintenance',label:'维护中'},{value:'retired',label:'已报废'}]
const reminderKinds = {service:'维护到期',retire:'报废到期',inspection:'巡检到期'}
const actionTitles = {inspect:'提交巡检结果',rectify:'提交整改',review:'复核整改',cancel:'取消巡检任务'}
const taskAssets = computed(() => options.extinguishers.filter(item => item.stationId === taskStationId.value && item.status !== 'retired'))
const assignees = computed(() => { const asset = options.extinguishers.find(item => item.id === taskForm.extinguisherId); return options.personnel.filter(item => item.stationId === asset?.stationId && item.enabled) })
const blank = () => ({id:'',code:'',stationId:filters.stationId,location:'',type:'dry_powder',specification:'',manufacturer:'',serialNumber:'',manufacturedOn:'',serviceDueOn:'',retireOn:'',inspectionCycleDays:30,status:'active',notes:''})
const stationName = id => options.stations.find(item=>item.id===id)?.name || '已移除消防站'
const personName = id => options.personnel.find(item=>item.id===id)?.name || '已移除人员'
const assetName = id => options.extinguishers.find(item=>item.id===id)?.code || '已移除灭火器'
const typeName = value => extinguisherTypes.find(item=>item.value===value)?.label || value
let loadVersion = 0, optionVersion = 0, statVersion = 0
function query() { return {q:filters.q,stationId:filters.stationId,status:filters.status,...(tab.value === 'assets' ? {due:filters.due,remindDays:filters.remindDays} : {})} }
async function load() {
  const version = ++loadVersion; loading.value = true; loadError.value = ''
  try {
    const result = await api(`/api/v1/${tab.value === 'assets'?'extinguishers':'extinguisher-inspections'}?${fireQuery(query(),{page:page.value,pageSize:pageSize.value})}`)
    if (version !== loadVersion) return
    rows.value = result.items || []; total.value = Number(result.total || 0)
  } catch (error) { if (version === loadVersion) {rows.value=[];total.value=0;loadError.value=errorMessage(error)} }
  finally { if (version === loadVersion) loading.value = false }
}
async function loadOptions() {
  const version = ++optionVersion; optionsError.value = ''
  try { const result = await api('/api/v1/fire-safety/options'); if (version === optionVersion) Object.assign(options,{stations:result.stations || [],personnel:result.personnel || [],extinguishers:result.extinguishers || [],inspectionChecks:result.inspectionChecks || []}) }
  catch (error) { if (version === optionVersion) optionsError.value = errorMessage(error) }
}
async function loadStatistics() {
  const version = ++statVersion; statisticsError.value = ''
  const filtersForStatistics = tab.value === 'assets' ? query() : {stationId:filters.stationId,remindDays:filters.remindDays}
  try { const result = await api(`/api/v1/extinguishers/statistics?${fireQuery(filtersForStatistics)}`); if (version === statVersion) statistics.value = result }
  catch (error) { if (version === statVersion) {statistics.value=null;statisticsError.value=errorMessage(error)} }
}
async function refresh() { await Promise.all([load(),loadOptions(),loadStatistics()]) }
function applyFilters() { page.value = 1; load(); loadStatistics() }
watch(tab, () => { page.value=1; rows.value=[]; filters.status=''; filters.due=''; applyFilters() })
function changePage(value) { page.value = value; load() }
function changePageSize(value) { pageSize.value = value; page.value = 1; load() }
function openAsset(row) {
  for (const key of Object.keys(form)) delete form[key]
  Object.assign(form,blank(),row ? JSON.parse(JSON.stringify(row)) : {})
  saveError.value=''; dialog.value=true; loadOptions()
}
async function saveAsset() {
  if (saving.value || !can(form.id?'PUT /api/v1/extinguishers/:id':'POST /api/v1/extinguishers')) return
  saving.value=true; saveError.value=''
  try {
    if (!form.code.trim() || !form.stationId || !form.location.trim()) throw new Error('请填写灭火器编号、放置位置并选择所属消防站')
    if (!Number.isInteger(form.inspectionCycleDays) || form.inspectionCycleDays < 1) throw new Error('巡检周期须为正整数天数')
    await api(`/api/v1/extinguishers${form.id?`/${encodeURIComponent(form.id)}`:''}`,{method:form.id?'PUT':'POST',body:JSON.stringify(managementPayload('extinguishers',form))})
    UiMessage.success('灭火器资料已保存'); dialog.value=false; if(assetDetail.value?.id===form.id)assetDetail.value=null; await refresh()
  } catch (error) {saveError.value=errorMessage(error)}
  finally {saving.value=false}
}
async function removeAsset(row) {
  if (deleting.value || !can('DELETE /api/v1/extinguishers/:id')) return
  deleting.value=true
  try {
    await UiMessageBox.confirm(`确定删除灭火器“${row.code}”？已有巡检记录的灭火器不能删除，可改为报废状态保留记录。`,'删除确认',{type:'warning',confirmButtonText:'确定删除',cancelButtonText:'取消'})
    await api(`/api/v1/extinguishers/${encodeURIComponent(row.id)}?version=${row.version}`,{method:'DELETE'})
    UiMessage.success('已删除'); if(rows.value.length===1 && page.value>1)page.value--; await refresh()
  } catch (error) {if(error!=='cancel' && error!=='close')notifyError(error)}
  finally {deleting.value=false}
}
async function openAssetDetail(row) {
  if (opening.value) return
  opening.value=true
  try {assetDetail.value=await api(`/api/v1/extinguishers/${encodeURIComponent(row.id)}?remindDays=${filters.remindDays}`)}
  catch(error){notifyError(error)} finally{opening.value=false}
}
function openCreateTask(asset) {
  taskStationId.value=asset?.stationId || filters.stationId
  Object.assign(taskForm,{extinguisherId:asset?.id || '',assigneeId:'',dueAtInput:localDateTimeInput(Date.now()+86400000),notes:''})
  taskError.value=''; taskDialog.value=true; loadOptions()
}
function changeTaskStation() {taskForm.extinguisherId='';taskForm.assigneeId=''}
async function saveTask() {
  if(taskSaving.value || !can('POST /api/v1/extinguisher-inspections'))return
  taskSaving.value=true;taskError.value=''
  try {
    if(!taskForm.extinguisherId || !taskForm.assigneeId)throw new Error('请选择灭火器和巡检人员')
    await api('/api/v1/extinguisher-inspections',{method:'POST',body:JSON.stringify({extinguisherId:taskForm.extinguisherId,assigneeId:taskForm.assigneeId,dueAt:inputTimestamp(taskForm.dueAtInput,'巡检截止时间'),notes:taskForm.notes.trim()})})
    UiMessage.success('巡检任务已创建'); taskDialog.value=false; tab.value='inspections'; await refresh()
  }catch(error){taskError.value=errorMessage(error)}finally{taskSaving.value=false}
}
async function openTask(row, kind = '') {
  if(opening.value)return
  opening.value=true
  try {
    const task=await api(`/api/v1/extinguisher-inspections/${encodeURIComponent(row.id)}`)
    if(!kind){taskDetail.value=task;return}
    if(kind==='review' && !canReviewInspection(task,session.user))throw new Error('整改提交人不能复核自己的整改，请由其他有权限的用户复核')
    actionTask.value=task;action.value=kind;actionError.value=''
    Object.assign(actionForm,{checks:options.inspectionChecks.map(name=>({name,passed:null,standard:true})),findings:'',action:'',approved:true,note:'',reason:''})
  }catch(error){notifyError(error)}finally{opening.value=false}
}
async function saveAction() {
  if(actionSaving.value || !can(`POST /api/v1/extinguisher-inspections/:id/${action.value}`))return
  actionSaving.value=true;actionError.value=''
  try {
    const version=actionTask.value.version; let payload
    if(action.value==='inspect')payload=inspectionPayload(actionForm,version)
    if(action.value==='rectify'){if(!actionForm.action.trim())throw new Error('请填写整改措施及完成情况');payload={version,action:actionForm.action.trim()}}
    if(action.value==='review'){
      if(!canReviewInspection(actionTask.value,session.user))throw new Error('整改提交人不能复核自己的整改')
      if(!actionForm.note.trim())throw new Error('请填写复核意见')
      payload={version,approved:actionForm.approved,note:actionForm.note.trim()}
    }
    if(action.value==='cancel'){if(!actionForm.reason.trim())throw new Error('请填写取消原因');payload={version,reason:actionForm.reason.trim()}}
    const result=await api(`/api/v1/extinguisher-inspections/${encodeURIComponent(actionTask.value.id)}/${action.value}`,{method:'POST',body:JSON.stringify(payload)})
    UiMessage.success('任务已更新');action.value='';if(taskDetail.value?.id===result.id)taskDetail.value=result;await refresh()
  }catch(error){actionError.value=errorMessage(error)}finally{actionSaving.value=false}
}
// 批量创建巡检：按消防站和到期情况选出灭火器，统一指定巡检人员和截止时间；已有未结任务或已报废的资产由服务端跳过。
const batchDialog = ref(false), batchForm = reactive({stationId:'',due:'overdue',assigneeId:'',dueAtInput:'',notes:''}), batchAssets = ref([]), batchSelected = ref([]), batchLoading = ref(false), batchSaving = ref(false), batchError = ref(''), batchResult = ref(null)
const batchAssignees = computed(() => options.personnel.filter(item => item.stationId === batchForm.stationId && item.enabled))
let batchVersion = 0
async function loadBatchAssets() {
  const version = ++batchVersion; batchAssets.value=[]; batchSelected.value=[]; batchError.value=''
  if (!batchForm.stationId) return
  batchLoading.value=true
  try {
    const result = await api(`/api/v1/extinguishers?${fireQuery({stationId:batchForm.stationId,due:batchForm.due,remindDays:filters.remindDays},{page:1,pageSize:100})}`)
    if (version !== batchVersion) return
    batchAssets.value = (result.items || []).filter(item => item.status !== 'retired' && !item.openInspection)
    batchSelected.value = batchAssets.value.map(item => item.id)
  } catch (error) { if (version === batchVersion) batchError.value = errorMessage(error) }
  finally { if (version === batchVersion) batchLoading.value=false }
}
function openBatch() {
  Object.assign(batchForm,{stationId:filters.stationId || options.stations.find(item=>item.enabled)?.id || '',due:'overdue',assigneeId:'',dueAtInput:localDateTimeInput(Date.now()+86400000),notes:''})
  batchResult.value=null; batchDialog.value=true; loadOptions(); loadBatchAssets()
}
function changeBatchStation() { batchForm.assigneeId=''; loadBatchAssets() }
function toggleBatchAsset(id, checked) { batchSelected.value = checked ? [...new Set([...batchSelected.value,id])] : batchSelected.value.filter(item => item !== id) }
async function saveBatch() {
  if (batchSaving.value || !can('POST /api/v1/extinguisher-inspections/batch')) return
  batchSaving.value=true; batchError.value=''
  try {
    if (!batchSelected.value.length || !batchForm.assigneeId) throw new Error('请选择灭火器和巡检人员')
    const result = await api('/api/v1/extinguisher-inspections/batch',{method:'POST',body:JSON.stringify({extinguisherIds:batchSelected.value,assigneeId:batchForm.assigneeId,dueAt:inputTimestamp(batchForm.dueAtInput,'巡检截止时间'),notes:batchForm.notes.trim()})})
    batchResult.value = result
    UiMessage.success(`已创建 ${result.created} 个巡检任务`)
    await Promise.all([refresh(), loadBatchAssets()])
  } catch (error) { batchError.value=errorMessage(error) }
  finally { batchSaving.value=false }
}
function assetActions(row){return [
  {key:'detail',label:'详情',disabled:opening.value,onClick:()=>openAssetDetail(row)},
  {key:'task',label:'创建巡检',permission:'POST /api/v1/extinguisher-inspections',hidden:row.status==='retired' || Boolean(row.openInspection),onClick:()=>openCreateTask(row)},
  {key:'edit',label:'编辑',permission:'PUT /api/v1/extinguishers/:id',onClick:()=>openAsset(row)},
  {key:'delete',label:'删除',permission:'DELETE /api/v1/extinguishers/:id',type:'danger',disabled:deleting.value,onClick:()=>removeAsset(row)}
]}
function taskActions(row){return [
  {key:'detail',label:'详情',disabled:opening.value,onClick:()=>openTask(row)},
  {key:'inspect',label:'巡检',permission:'POST /api/v1/extinguisher-inspections/:id/inspect',hidden:row.status!=='pending',disabled:opening.value,onClick:()=>openTask(row,'inspect')},
  {key:'rectify',label:'整改',permission:'POST /api/v1/extinguisher-inspections/:id/rectify',hidden:row.status!=='rectifying',disabled:opening.value,onClick:()=>openTask(row,'rectify')},
  {key:'review',label:'复核',permission:'POST /api/v1/extinguisher-inspections/:id/review',hidden:row.status!=='reviewing',disabled:opening.value || !canReviewInspection(row,session.user),onClick:()=>openTask(row,'review')},
  {key:'cancel',label:'取消',permission:'POST /api/v1/extinguisher-inspections/:id/cancel',hidden:['completed','cancelled'].includes(row.status),disabled:opening.value,type:'danger',onClick:()=>openTask(row,'cancel')}
]}
onMounted(refresh)
</script>

<template>
  <div class="fire-page">
    <div v-if="optionsError" class="fire-error" role="alert"><span>{{optionsError}}</span><ui-button size="small" @click="loadOptions">重新加载选项</ui-button></div>
    <ui-tabs v-model="tab"><ui-tab-pane name="assets" label="灭火器台账" /><ui-tab-pane name="inspections" label="巡检与整改" /></ui-tabs>
    <FilterBar>
      <ui-input v-model="filters.q" clearable :placeholder="tab==='assets'?'搜索编号、型号或位置':'搜索巡检备注或问题'" aria-label="搜索关键字" @keyup.enter="applyFilters" />
      <ui-select v-model="filters.stationId" clearable filterable placeholder="全部消防站" aria-label="消防站筛选" @change="applyFilters"><ui-option v-for="item in options.stations" :key="item.id" :value="item.id" :label="item.name" /></ui-select>
      <ui-select v-model="filters.status" clearable placeholder="全部状态" aria-label="状态筛选" @change="applyFilters"><ui-option v-for="item in tab==='assets'?assetStates:inspectionStates" :key="item.value" :value="item.value" :label="item.label" /></ui-select>
      <ui-select v-if="tab==='assets'" v-model="filters.due" clearable placeholder="全部到期情况" aria-label="到期筛选" @change="applyFilters"><ui-option value="overdue" label="已逾期" /><ui-option value="soon" label="即将到期" /></ui-select><ui-button @click="applyFilters">查询</ui-button>
      <template #actions><ui-button :loading="loading" @click="refresh"><RefreshCw />刷新</ui-button><ui-button v-permission="'POST /api/v1/extinguisher-inspections/batch'" :disabled="Boolean(optionsError)" @click="openBatch">批量创建巡检</ui-button><ui-button v-if="tab==='assets'" v-permission="'POST /api/v1/extinguishers'" type="primary" :disabled="Boolean(optionsError)" @click="openAsset()"><Plus />新增灭火器</ui-button><ui-button v-else v-permission="'POST /api/v1/extinguisher-inspections'" type="primary" :disabled="Boolean(optionsError)" @click="openCreateTask()"><Plus />创建巡检任务</ui-button></template>
    </FilterBar>
    <div v-if="tab==='assets'" class="fire-row"><span class="fire-hint" style="margin:0">提前提醒天数</span><ui-input-number v-model="filters.remindDays" :min="0" :max="365" :precision="0" @change="applyFilters" /></div>
    <div v-if="statisticsError" class="fire-error" role="alert"><span>{{statisticsError}}</span><ui-button size="small" @click="loadStatistics">重新加载统计</ui-button></div>
    <div v-if="statistics" class="fire-stats" aria-label="灭火器概况"><div v-for="(label,key) in tab==='assets'?{total:'灭火器',active:'在用',maintenance:'维护中',retired:'已报废',overdue:'已逾期',soon:'即将到期'}:{pendingInspections:'待巡检',overdueInspections:'逾期任务',rectifying:'待整改',reviewing:'待复核'}" :key="key" class="fire-stat" :class="{'fire-stat--danger':['overdue','overdueInspections','rectifying'].includes(key)}"><span>{{label}}</span><strong>{{statistics[key] ?? '—'}}</strong></div></div>
    <p v-if="tab==='inspections' && statistics" class="fire-hint">概况统计所选消防站的全部巡检任务；下方任务列表按关键字和状态筛选。</p>
    <DataTableCard :title="`${tab==='assets'?'灭火器':'巡检任务'} · ${total} 条`" :page="page" :page-size="pageSize" :total="total" :error="loadError" @retry="load" @update:page="changePage" @update:page-size="changePageSize">
      <ui-table :data="rows" :loading="loading" :empty-text="tab==='assets'?'暂无灭火器，请添加现场台账':'暂无巡检任务，请创建巡检任务'">
        <template v-if="tab==='assets'">
          <ui-table-column label="灭火器" min-width="180"><template #default="{row}"><button class="fire-name" type="button" @click="openAssetDetail(row)">{{row.code}}</button><small class="fire-subline">{{typeName(row.type)}} · {{row.specification || '规格未填'}}</small></template></ui-table-column>
          <ui-table-column label="所属消防站 / 位置" min-width="200"><template #default="{row}">{{stationName(row.stationId)}}<small class="fire-subline">{{row.location}}</small></template></ui-table-column>
          <ui-table-column label="到期提醒" min-width="230"><template #default="{row}"><span v-for="item in row.reminders || []" :key="item.kind" class="fire-reminder" :class="`fire-reminder--${item.status}`">{{reminderKinds[item.kind]}} · {{dateLabel(item.dueOn)}}{{item.status==='overdue'?'（已逾期）':item.status==='soon'?'（即将到期）':''}}</span><span v-if="!row.reminders?.length">—</span></template></ui-table-column>
          <ui-table-column label="最近巡检" min-width="170"><template #default="{row}">{{row.lastInspectedAt?dateTimeLabel(row.lastInspectedAt):'尚未巡检'}}<small class="fire-subline">周期 {{row.inspectionCycleDays}} 天</small></template></ui-table-column>
          <ui-table-column label="状态" width="120"><template #default="{row}"><StatusDot :tone="statusTone(row.status)" :label="statusLabel(row.status)" /><small v-if="row.openInspection" class="fire-subline" :class="{'fire-reminder--overdue':row.openInspection.status==='rectifying'}">{{inspectionStates.find(item=>item.value===row.openInspection.status)?.label}}</small></template></ui-table-column>
        </template>
        <template v-else>
          <ui-table-column label="巡检资产" min-width="180"><template #default="{row}"><button class="fire-name" type="button" @click="openTask(row)">{{assetName(row.extinguisherId)}}</button><small class="fire-subline">{{stationName(options.extinguishers.find(item=>item.id===row.extinguisherId)?.stationId)}}</small></template></ui-table-column><ui-table-column label="巡检人员" min-width="130"><template #default="{row}">{{personName(row.assigneeId)}}</template></ui-table-column><ui-table-column label="截止时间" min-width="170"><template #default="{row}"><span :class="{'fire-reminder--overdue':!['completed','cancelled'].includes(row.status) && row.dueAt<Date.now()}">{{dateTimeLabel(row.dueAt)}}</span></template></ui-table-column><ui-table-column label="状态 / 结果" min-width="120"><template #default="{row}"><StatusDot :tone="statusTone(row.status)" :label="inspectionStates.find(item=>item.value===row.status)?.label || row.status" /><small v-if="row.result" class="fire-subline">{{statusLabel(row.result)}}</small></template></ui-table-column><ui-table-column label="问题 / 备注" min-width="220" show-overflow-tooltip><template #default="{row}">{{row.findings || row.notes || '—'}}</template></ui-table-column>
        </template>
        <ui-table-column label="操作" :width="tab==='assets'?190:170" align="right" fixed="right"><template #default="{row}"><RowActions :actions="tab==='assets'?assetActions(row):taskActions(row)" /></template></ui-table-column>
      </ui-table>
    </DataTableCard>

    <ui-dialog v-model="dialog" :title="form.id?'编辑灭火器':'新增灭火器'" width="min(760px,94vw)" :close-on-click-modal="!saving" :show-close="!saving" :close-on-press-escape="!saving">
      <ui-alert v-if="saveError" type="error" :title="saveError" :closable="false" class="fire-section" />
      <ui-form :model="form" label-position="top">
        <section class="fire-section"><h3>资产与放置位置</h3><div class="fire-form-grid"><ui-form-item label="灭火器编号 *"><ui-input v-model="form.code" :disabled="saving" /></ui-form-item><ui-form-item label="所属消防站 *"><ui-select v-model="form.stationId" :disabled="saving" filterable><ui-option v-for="item in options.stations" :key="item.id" :label="`${item.name}${!item.enabled?'（已停用）':''}`" :value="item.id" :disabled="!item.enabled && item.id!==form.stationId" /></ui-select></ui-form-item><ui-form-item label="放置位置 *"><ui-input v-model="form.location" :disabled="saving" placeholder="建筑、楼层或具体点位" /></ui-form-item><ui-form-item label="灭火器类型"><ui-select v-model="form.type" :disabled="saving"><ui-option v-for="item in extinguisherTypes" :key="item.value" :value="item.value" :label="item.label" /></ui-select></ui-form-item><ui-form-item label="规格"><ui-input v-model="form.specification" :disabled="saving" placeholder="例如：4 kg" /></ui-form-item><ui-form-item label="状态"><ui-select v-model="form.status" :disabled="saving"><ui-option v-for="item in assetStates" :key="item.value" :value="item.value" :label="item.label" /></ui-select></ui-form-item></div></section>
        <section class="fire-section"><h3>生产与维护计划</h3><div class="fire-form-grid"><ui-form-item label="生产厂家"><ui-input v-model="form.manufacturer" :disabled="saving" /></ui-form-item><ui-form-item label="出厂序列号"><ui-input v-model="form.serialNumber" :disabled="saving" /></ui-form-item><ui-form-item label="生产日期"><input v-model="form.manufacturedOn" type="date" class="fire-date" :disabled="saving" /></ui-form-item><ui-form-item label="维护到期日期"><input v-model="form.serviceDueOn" type="date" class="fire-date" :disabled="saving" /></ui-form-item><ui-form-item label="计划报废日期"><input v-model="form.retireOn" type="date" class="fire-date" :disabled="saving" /></ui-form-item><ui-form-item label="巡检周期（天）"><ui-input-number v-model="form.inspectionCycleDays" :disabled="saving" :min="1" :max="3650" :precision="0" /></ui-form-item></div></section>
        <ui-form-item label="备注"><ui-input v-model="form.notes" :disabled="saving" type="textarea" :rows="3" maxlength="2000" /></ui-form-item>
      </ui-form><template #footer><ui-button :disabled="saving" @click="dialog=false">取消</ui-button><ui-button v-permission="form.id?'PUT /api/v1/extinguishers/:id':'POST /api/v1/extinguishers'" type="primary" :loading="saving" @click="saveAsset">保存</ui-button></template>
    </ui-dialog>
    <ui-dialog v-model="batchDialog" title="批量创建巡检任务" width="min(760px,94vw)" :close-on-click-modal="!batchSaving" :show-close="!batchSaving" :close-on-press-escape="!batchSaving">
      <ui-alert v-if="batchError" type="error" :title="batchError" :closable="false" class="fire-section" />
      <ui-form label-position="top"><div class="fire-form-grid">
        <ui-form-item label="消防站 *"><ui-select v-model="batchForm.stationId" :disabled="batchSaving" filterable @change="changeBatchStation"><ui-option v-for="item in options.stations" :key="item.id" :label="item.name" :value="item.id" :disabled="!item.enabled" /></ui-select></ui-form-item>
        <ui-form-item label="到期情况"><ui-select v-model="batchForm.due" :disabled="batchSaving" @change="loadBatchAssets"><ui-option value="overdue" label="已逾期" /><ui-option value="soon" label="即将到期" /><ui-option value="" label="全部在用" /></ui-select></ui-form-item>
        <ui-form-item label="巡检人员 *"><ui-select v-model="batchForm.assigneeId" :disabled="batchSaving || !batchForm.stationId" filterable placeholder="选择本消防站人员"><ui-option v-for="item in batchAssignees" :key="item.id" :label="item.name" :value="item.id" /></ui-select></ui-form-item>
        <ui-form-item label="巡检截止时间 *"><input v-model="batchForm.dueAtInput" :disabled="batchSaving" type="datetime-local" class="fire-date" /></ui-form-item>
      </div>
      <section class="fire-section"><h3>选择灭火器 · 已选 {{batchSelected.length}} / {{batchAssets.length}}</h3><p class="fire-hint">只列出没有未结巡检任务的在用灭火器（最多 100 个）。</p>
        <p v-if="batchLoading" class="fire-hint">正在读取灭火器…</p>
        <div v-else class="fire-batch-assets"><label v-for="item in batchAssets" :key="item.id" class="fire-batch-asset"><ui-checkbox :model-value="batchSelected.includes(item.id)" :disabled="batchSaving" @update:model-value="checked=>toggleBatchAsset(item.id,checked)" /><span><strong>{{item.code}}</strong><small class="fire-subline">{{item.location}}</small></span></label><p v-if="!batchAssets.length" class="fire-hint">没有符合条件的灭火器。</p></div>
      </section>
      <ui-form-item label="备注"><ui-input v-model="batchForm.notes" :disabled="batchSaving" type="textarea" :rows="2" maxlength="2000" /></ui-form-item></ui-form>
      <ui-alert v-if="batchResult?.skipped?.length" type="warning" :closable="false" :title="`已跳过 ${batchResult.skipped.length} 个：${batchResult.skipped.map(item=>`${item.code}（${item.reason}）`).join('、')}`" />
      <template #footer><ui-button :disabled="batchSaving" @click="batchDialog=false">关闭</ui-button><ui-button v-permission="'POST /api/v1/extinguisher-inspections/batch'" type="primary" :loading="batchSaving" :disabled="!batchSelected.length || !batchForm.assigneeId" @click="saveBatch">创建 {{batchSelected.length}} 个任务</ui-button></template>
    </ui-dialog>
    <ui-dialog v-model="taskDialog" title="创建巡检任务" width="min(580px,94vw)" :close-on-click-modal="!taskSaving" :show-close="!taskSaving" :close-on-press-escape="!taskSaving">
      <ui-alert v-if="taskError" type="error" :title="taskError" :closable="false" class="fire-section" /><ui-form label-position="top"><ui-form-item label="消防站"><ui-select v-model="taskStationId" :disabled="taskSaving" filterable @change="changeTaskStation"><ui-option v-for="item in options.stations" :key="item.id" :label="item.name" :value="item.id" :disabled="!item.enabled" /></ui-select></ui-form-item><ui-form-item label="灭火器 *"><ui-select v-model="taskForm.extinguisherId" :disabled="taskSaving || !taskStationId" filterable @change="taskForm.assigneeId=''" placeholder="选择该消防站的灭火器"><ui-option v-for="item in taskAssets" :key="item.id" :label="item.code" :value="item.id" /></ui-select></ui-form-item><ui-form-item label="巡检人员 *"><ui-select v-model="taskForm.assigneeId" :disabled="taskSaving || !taskForm.extinguisherId" filterable placeholder="选择本消防站人员"><ui-option v-for="item in assignees" :key="item.id" :label="item.name" :value="item.id" /></ui-select></ui-form-item><ui-form-item label="巡检截止时间 *"><input v-model="taskForm.dueAtInput" :disabled="taskSaving" type="datetime-local" class="fire-date" /></ui-form-item><ui-form-item label="任务说明"><ui-input v-model="taskForm.notes" :disabled="taskSaving" type="textarea" :rows="3" maxlength="2000" /></ui-form-item></ui-form><template #footer><ui-button :disabled="taskSaving" @click="taskDialog=false">取消</ui-button><ui-button v-permission="'POST /api/v1/extinguisher-inspections'" type="primary" :loading="taskSaving" @click="saveTask">创建任务</ui-button></template>
    </ui-dialog>
    <ui-dialog :model-value="Boolean(action)" :title="actionTitles[action] || ''" width="min(680px,94vw)" :close-on-click-modal="!actionSaving" :show-close="!actionSaving" :close-on-press-escape="!actionSaving" @update:model-value="value=>{if(!value && !actionSaving)action=''}">
      <ui-alert v-if="actionError" type="error" :title="actionError" :closable="false" class="fire-section" /><p class="fire-hint">灭火器 {{assetName(actionTask?.extinguisherId)}} · {{personName(actionTask?.assigneeId)}}</p>
      <ui-form label-position="top">
        <template v-if="action==='inspect'"><p class="fire-hint">按实际检查结果逐项选择。标准项目不能修改或移除，可追加其他检查项目。</p><div v-for="(item,index) in actionForm.checks" :key="index" class="fire-row"><ui-input v-model="item.name" :disabled="actionSaving || item.standard" placeholder="检查项目名称" :aria-label="`检查项目 ${index+1}`" /><ui-select v-model="item.passed" :disabled="actionSaving" placeholder="请选择结果" :aria-label="`检查结果 ${index+1}`"><ui-option :value="true" label="合格" /><ui-option :value="false" label="不合格" /></ui-select><ui-button v-if="!item.standard" size="small" :disabled="actionSaving" @click="actionForm.checks.splice(index,1)">移除</ui-button></div><ui-button size="small" :disabled="actionSaving" @click="actionForm.checks.push({name:'',passed:null})">添加检查项目</ui-button><ui-form-item label="发现的问题（不合格时必填）" style="margin-top:16px"><ui-input v-model="actionForm.findings" :disabled="actionSaving" type="textarea" :rows="4" maxlength="4000" /></ui-form-item></template>
        <template v-if="action==='rectify'"><ui-alert type="warning" :title="actionTask?.findings || '请根据巡检问题完成整改'" :closable="false" class="fire-section" /><ui-form-item label="整改措施与完成情况 *"><ui-input v-model="actionForm.action" :disabled="actionSaving" type="textarea" :rows="5" maxlength="4000" /></ui-form-item></template>
        <template v-if="action==='review'"><div class="fire-history"><strong>本次整改</strong><p>{{actionTask?.rectifications?.at(-1)?.action}}</p><small class="fire-hint">提交人 {{actionTask?.rectifications?.at(-1)?.submittedBy}} · {{dateTimeLabel(actionTask?.rectifications?.at(-1)?.submittedAt)}}</small></div><ui-form-item label="复核结果"><ui-select v-model="actionForm.approved" :disabled="actionSaving"><ui-option :value="true" label="通过，关闭任务" /><ui-option :value="false" label="驳回，继续整改" /></ui-select></ui-form-item><ui-form-item label="复核意见 *"><ui-input v-model="actionForm.note" :disabled="actionSaving" type="textarea" :rows="4" maxlength="4000" /></ui-form-item></template>
        <ui-form-item v-if="action==='cancel'" label="取消原因 *"><ui-input v-model="actionForm.reason" :disabled="actionSaving" type="textarea" :rows="4" maxlength="2000" /></ui-form-item>
      </ui-form><template #footer><ui-button :disabled="actionSaving" @click="action=''">返回</ui-button><ui-button v-permission="`POST /api/v1/extinguisher-inspections/:id/${action}`" type="primary" :loading="actionSaving" @click="saveAction">提交</ui-button></template>
    </ui-dialog>
    <ui-drawer :model-value="Boolean(assetDetail)" :title="assetDetail?`灭火器 · ${assetDetail.code}`:''" size="min(700px,100vw)" @close="assetDetail=null">
      <template v-if="assetDetail"><StatusDot :tone="statusTone(assetDetail.status)" :label="statusLabel(assetDetail.status)" /><dl class="fire-details"><div v-for="(value,label) in {'所属消防站':stationName(assetDetail.stationId),'放置位置':assetDetail.location,'类型':typeName(assetDetail.type),'规格':assetDetail.specification,'厂家':assetDetail.manufacturer,'出厂序列号':assetDetail.serialNumber,'生产日期':dateLabel(assetDetail.manufacturedOn),'维护到期':dateLabel(assetDetail.serviceDueOn),'计划报废':dateLabel(assetDetail.retireOn),'巡检周期':`${assetDetail.inspectionCycleDays} 天`,'最近巡检':assetDetail.lastInspectedAt?dateTimeLabel(assetDetail.lastInspectedAt):'尚未巡检','下次巡检':dateLabel(assetDetail.nextInspectionOn),'备注':assetDetail.notes}" :key="label"><dt>{{label}}</dt><dd>{{value || '—'}}</dd></div></dl><div class="fire-row"><ui-button v-permission="'PUT /api/v1/extinguishers/:id'" @click="openAsset(assetDetail)">编辑资料</ui-button><ui-button v-if="assetDetail.status!=='retired'" v-permission="'POST /api/v1/extinguisher-inspections'" type="primary" @click="openCreateTask(assetDetail)">创建巡检任务</ui-button></div></template>
    </ui-drawer>
    <ui-drawer :model-value="Boolean(taskDetail)" :title="taskDetail?`巡检 · ${assetName(taskDetail.extinguisherId)}`:''" size="min(760px,100vw)" @close="taskDetail=null">
      <template v-if="taskDetail"><StatusDot :tone="statusTone(taskDetail.status)" :label="inspectionStates.find(item=>item.value===taskDetail.status)?.label || taskDetail.status" /><div class="fire-row"><RowActions :actions="taskActions(taskDetail).filter(item=>item.key!=='detail')" :inline="4" /></div><p v-if="taskDetail.status==='reviewing' && !canReviewInspection(taskDetail,session.user)" class="fire-hint">当前整改由你提交，请由其他有复核权限的用户复核。</p><dl class="fire-details"><div><dt>巡检人员</dt><dd>{{personName(taskDetail.assigneeId)}}</dd></div><div><dt>截止时间</dt><dd>{{dateTimeLabel(taskDetail.dueAt)}}</dd></div><div><dt>任务说明</dt><dd>{{taskDetail.notes || '—'}}</dd></div><div><dt>创建人</dt><dd>{{taskDetail.createdBy}}</dd></div><div v-if="taskDetail.inspectedAt"><dt>巡检时间 / 提交人</dt><dd>{{dateTimeLabel(taskDetail.inspectedAt)}} · {{taskDetail.inspectedBy}}</dd></div><div v-if="taskDetail.result"><dt>巡检结果</dt><dd>{{statusLabel(taskDetail.result)}}</dd></div><div v-if="taskDetail.findings"><dt>发现的问题</dt><dd>{{taskDetail.findings}}</dd></div><div v-if="taskDetail.cancelReason"><dt>取消原因</dt><dd>{{taskDetail.cancelReason}}</dd></div></dl>
        <section v-if="taskDetail.checks?.length" class="fire-section"><h3>检查项目</h3><div v-for="item in taskDetail.checks" :key="item.name" class="fire-row"><span>{{item.name}}</span><StatusDot :tone="item.passed?'success':'danger'" :label="item.passed?'合格':'不合格'" /></div></section>
        <section class="fire-section"><h3>整改与复核记录</h3><p v-if="!taskDetail.rectifications?.length" class="fire-hint">暂无整改记录</p><article v-for="(item,index) in taskDetail.rectifications || []" :key="index" class="fire-history"><strong>第 {{index+1}} 次整改</strong><StatusDot :tone="statusTone(item.status)" :label="item.status==='pending'?'待复核':statusLabel(item.status)" /><p>{{item.action}}</p><small class="fire-hint">{{item.submittedBy}} · {{dateTimeLabel(item.submittedAt)}}</small><template v-if="item.reviewedAt"><p>复核意见：{{item.reviewNote || '—'}}</p><small class="fire-hint">{{item.reviewedBy}} · {{dateTimeLabel(item.reviewedAt)}}</small></template></article></section>
      </template>
    </ui-drawer>
  </div>
</template>

<style scoped src="../fireSafetyManagement.css"></style>
