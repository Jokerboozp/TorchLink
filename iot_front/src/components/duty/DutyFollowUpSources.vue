<script setup>
import {onBeforeUnmount,onMounted,ref,watch} from 'vue'
import {RefreshCw} from '@lucide/vue'
import {api} from '../../api.js'
import {can} from '../../permissions.js'
import {dutyTime,statusText} from '../../duty/state.js'
import DataTableCard from '../layout/DataTableCard.vue'
defineProps({catalog:Object})
const emit=defineEmits(['navigate'])
const verificationText=value=>({PENDING:"待验收",PASSED:"已通过功能验收",FAILED:"功能验收失败",UNKNOWN:"功能验收未知"}[value]||"功能验收未知")
const sourceStatus=value=>({DRAFT:'草稿',COMPLETED:'工作记录结束'}[value]||statusText(value))
const rows=ref([]),total=ref(0),page=ref(1),pageSize=ref(20),loading=ref(false),error=ref(''),kind=ref(''),status=ref('')
let generation=0
async function load(){const token=++generation;loading.value=true;error.value='';try{const query=new URLSearchParams({limit:String(pageSize.value),offset:String((page.value-1)*pageSize.value)});if(kind.value)query.set('sourceKind',kind.value);if(status.value)query.set('status',status.value);const data=await api(`/api/v1/follow-up-sources?${query}`);if(token!==generation)return;rows.value=data.items||[];total.value=data.total||0}catch(cause){if(token===generation){rows.value=[];total.value=0;error.value=cause.message}}finally{if(token===generation)loading.value=false}}
function open(row){emit('navigate',row.sourceKind==='CORRECTIVE_ACTION'?'response':'maintenance',row.sourceKind==='CORRECTIVE_ACTION'?{correctiveId:row.resourceId}:{maintenanceRecordId:row.resourceId})}
watch([kind,status,pageSize],()=>{page.value=1;load()});watch(page,load)
onMounted(load);onBeforeUnmount(()=>generation++)
</script>
<template><section class="duty-panel"><div class="duty-toolbar"><div><h3 class="follow-up-title">业务主事项</h3><p class="duty-hint">主责与验收结果取自整改或维修记录。当班跟进保留自己的处理记录。</p></div><ui-button size="small" :loading="loading" @click="load"><RefreshCw/>刷新</ui-button></div><div class="follow-up-filters"><ui-select v-model="kind" clearable placeholder="全部来源"><ui-option v-if="can('menu:response')" value="CORRECTIVE_ACTION" label="处置复盘整改"/><ui-option v-if="can('menu:maintenance')" value="MAINTENANCE_RECORD" label="维修记录"/></ui-select><ui-select v-model="status" clearable placeholder="全部业务状态"><ui-option v-for="value in ['DRAFT','OPEN','IN_PROGRESS','PENDING_VERIFICATION','COMPLETED','DONE','CANCELLED']" :key="value" :value="value" :label="sourceStatus(value)"/></ui-select></div><DataTableCard title="主业务记录" :total="total" :page="page" :page-size="pageSize" :error="error" @retry="load" @update:page="page=$event" @update:page-size="pageSize=$event"><ui-table v-loading="loading" :data="rows" empty-text="暂无可读的业务主事项"><ui-table-column label="来源 / 内容" min-width="240"><template #default="{row}"><strong>{{row.title}}</strong><p class="duty-meta">{{row.sourceKind==='CORRECTIVE_ACTION'?'处置复盘整改':'维修记录'}} · 版本 {{row.version}}</p><p class="duty-meta">{{row.deviceIds.map(catalog.deviceName).join('、')}}</p></template></ui-table-column><ui-table-column label="业务主责 / 状态" min-width="170"><template #default="{row}">{{catalog.userName(row.owner)}}<p>{{sourceStatus(row.status)}}</p><p v-if="row.verification" class="duty-meta">{{verificationText(row.verification)}}</p></template></ui-table-column><ui-table-column label="约定期限" min-width="170"><template #default="{row}">{{dutyTime(row.dueAt)}}</template></ui-table-column><ui-table-column label="操作" width="110"><template #default="{row}"><ui-button text size="small" @click="open(row)">查看主记录</ui-button></template></ui-table-column></ui-table></DataTableCard></section></template>
<style scoped>.follow-up-title{font-size:15px;margin:0}.follow-up-filters{display:flex;flex-wrap:wrap;gap:8px}.follow-up-filters>*{max-width:260px;min-width:180px}@media(max-width:640px){.follow-up-filters>*{flex:1;max-width:none;min-width:120px}}</style>
