<script setup>
import {computed,onMounted,reactive,ref} from 'vue'
import {UiMessage,UiMessageBox} from '../ui/feedback.js'
import {api,notifyError,session} from '../api'
import DeviceScopePicker from '../components/DeviceScopePicker.vue'
import { userAccessPayload } from '../userAccess'
import { roleDeviceScope } from '../permissionPresets'
import { createClientId } from '../clientId'
import PermissionPicker from '../components/PermissionPicker.vue'
import { Plus, RefreshCw } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
defineEmits(['navigate'])
const tab=ref('roles'),users=ref([]),roles=ref([]),catalog=ref([]),loading=ref(false),saving=ref(false),dialog=ref(''),editing=ref(false)
const user=reactive(userAccessPayload())
const role=reactive({id:'',name:'',description:'',permissions:[],deviceScope:'none',deviceIds:[]})
const tenantId=ref(session.tenant),deviceOptions=ref([]),devicesLoading=ref(false),devicesError=ref('')
const plainScopeLabel=value=>value.deviceScope==='all'?'当前租户全部设备':value.deviceScope==='selected'?`指定 ${value.deviceIds?.length||0} 台设备`:'无设备'
const scopeLabel = value => value.deviceScope === 'inherit' ? `继承角色 · ${plainScopeLabel(roleDeviceScope(value.roleIds || [], roles.value))}` : plainScopeLabel(value)
const inheritedLabel = computed(() => plainScopeLabel(roleDeviceScope(user.roleIds, roles.value)))
const password=reactive({username:'',value:''})
let loadVersion=0
const canViewDevices = computed(() => user.permissions.includes('menu:devices') || roles.value.some(role => user.roleIds.includes(role.id) && role.permissions.includes('menu:devices')))
const deviceSelectionPending = computed(() => (dialog.value === 'role' ? role.deviceScope : user.deviceScope) === 'selected' && (devicesLoading.value || !!devicesError.value))
let deviceLoadVersion = 0
async function loadDevices() {
 const version = ++deviceLoadVersion
 devicesLoading.value = true
 devicesError.value = ''
 try {
  const data = await api('/api/v1/access/device-options')
  if (version === deviceLoadVersion) deviceOptions.value = data.items || []
 } catch { if (version === deviceLoadVersion) devicesError.value = '设备列表加载失败，请重新加载后再保存。' }
 finally { if (version === deviceLoadVersion) devicesLoading.value = false }
}
async function load() {
 const version=++loadVersion;loading.value=true
 try {
  const [u,r,p]=await Promise.all([api('/api/v1/access/users'),api('/api/v1/access/roles'),api('/api/v1/access/permissions')])
  if(version!==loadVersion)return
  tenantId.value=u.tenantId;users.value=u.items||[];roles.value=r.items||[];catalog.value=p.items||[]
 } catch(e) { if(version===loadVersion)notifyError(e) }
 finally { if(version===loadVersion)loading.value=false }
}
function editUser(value) {
 editing.value=!!value
 Object.assign(user,userAccessPayload(value || {deviceScope:'inherit'}))
 dialog.value='user'
 void loadDevices()
}
function enableDeviceMenu() { const target = dialog.value === 'role' ? role : user; target.permissions=[...new Set([...target.permissions,'menu:devices'])] }

function editRole(value) {
 editing.value=!!value
 Object.assign(role, {
  id:value?.id || `role-${createClientId()}`, name:value?.name || '', description:value?.description || '',
  permissions:[...(value?.permissions || [])], deviceScope:value?.deviceScope || 'none', deviceIds:[...(value?.deviceIds || [])]
 })
 dialog.value='role'
 void loadDevices()
}
async function save(){if(saving.value||deviceSelectionPending.value)return;saving.value=true;try{const isUser=dialog.value==='user',value=isUser?userAccessPayload(user):role;const base=isUser?'/api/v1/access/users':'/api/v1/access/roles';await api(base+(editing.value?'/'+encodeURIComponent(isUser?user.username:role.id):''),{method:editing.value?'PUT':'POST',body:JSON.stringify(value)});dialog.value='';user.password='';await load();UiMessage.success('已保存')}catch(e){notifyError(e)}finally{saving.value=false}}
async function remove(kind,value){try{await UiMessageBox.confirm(`确认删除${kind==='users'?'用户':'角色'}“${value.displayName||value.name||value.username}”？`,'删除确认',{type:'warning'});await api(`/api/v1/access/${kind}/${encodeURIComponent(value.username||value.id)}`,{method:'DELETE'});await load()}catch(e){if(e!=='cancel'&&e!=='close')notifyError(e)}}
function reset(value){password.username=value.username;password.value='';dialog.value='password'}
async function savePassword(){saving.value=true;try{await api(`/api/v1/access/users/${encodeURIComponent(password.username)}/password`,{method:'POST',body:JSON.stringify({password:password.value})});password.value='';dialog.value='';UiMessage.success('密码已重置，旧登录已失效')}catch(e){notifyError(e)}finally{saving.value=false}}
onMounted(load)
function userActions(row) {
  const self = row.username === session.user
  return [
    { key:'edit', label:'编辑', permission:'PUT /api/v1/access/users/:id', disabled:self, onClick:() => editUser(row) },
    { key:'password', label:'重置密码', permission:'POST /api/v1/access/users/:id/password', onClick:() => reset(row) },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/access/users/:id', disabled:self, onClick:() => remove('users',row) }
  ]
}
function roleActions(row) {
  return [
    { key:'edit', label:'编辑', permission:'PUT /api/v1/access/roles/:id', onClick:() => editRole(row) },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/access/roles/:id', onClick:() => remove('roles',row) }
  ]
}
</script>
<template>
 <ui-tabs v-model="tab"><ui-tab-pane label="角色管理" name="roles"/><ui-tab-pane label="用户管理" name="users"/><ui-tab-pane label="权限说明" name="permissions"/></ui-tabs>
 <FilterBar>
  <p class="access-tenant">当前租户：{{tenantId}}。先为角色配置功能和设备，再给用户分配角色。</p>
  <template #actions>
   <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
   <ui-button v-if="tab==='users'" v-permission="'POST /api/v1/access/users'" type="primary" @click="editUser()"><Plus />添加用户</ui-button>
   <ui-button v-if="tab==='roles'" v-permission="'POST /api/v1/access/roles'" type="primary" @click="editRole()"><Plus />添加角色</ui-button>
  </template>
 </FilterBar>
 <DataTableCard v-if="tab==='users'" :title="`用户 · ${users.length} 个`"><ui-table v-loading="loading" :data="users" empty-text="暂无用户，点击添加用户创建登录账户"><ui-table-column prop="username" label="用户名"/><ui-table-column prop="displayName" label="姓名 / 显示名称"/><ui-table-column label="设备访问范围"><template #default="{row}">{{scopeLabel(row)}}</template></ui-table-column><ui-table-column label="角色"><template #default="{row}">{{row.roleIds.map(id=>roles.find(r=>r.id===id)?.name||id).join('、')||'未分配'}}</template></ui-table-column><ui-table-column label="状态" width="100"><template #default="{row}"><StatusDot :tone="row.enabled?'success':'neutral'" :label="row.enabled?'启用':'停用'" /></template></ui-table-column><ui-table-column label="操作" width="240" fixed="right" align="right"><template #default="{row}"><RowActions :actions="userActions(row)" /></template></ui-table-column></ui-table></DataTableCard>
 <DataTableCard v-if="tab==='roles'" :title="`角色 · ${roles.length} 个`"><ui-table v-loading="loading" :data="roles" empty-text="暂无角色，点击添加角色配置权限"><ui-table-column prop="name" label="角色名称"/><ui-table-column label="说明"><template #default="{row}">{{row.description||'—'}}</template></ui-table-column><ui-table-column label="可用功能"><template #default="{row}">{{row.permissions.filter(p=>p.startsWith('menu:')).length}} 项功能</template></ui-table-column><ui-table-column label="设备访问范围"><template #default="{row}">{{scopeLabel(row)}}</template></ui-table-column><ui-table-column label="操作" width="160" fixed="right" align="right"><template #default="{row}"><RowActions :actions="roleActions(row)" /></template></ui-table-column></ui-table></DataTableCard>
 <ui-card v-if="tab==='permissions'" class="top-gap"><h3>通过角色统一授权</h3><p>角色同时配置可用功能和可见设备。用户默认继承角色的设备范围，多个角色取并集；功能选择“查看 / 问答”或“管理”，仅特殊需要才展开细项。</p><p>用户的“单独设置”会替代角色设备范围，适用于例外情况。已有用户保留原设备范围，可在编辑时切换为“继承角色”。后端每次请求均使用最新配置。</p><p>修改用户、停用用户或重置密码会使旧登录失效。角色被用户使用时不能删除。</p><p>设备列表、告警、原始报文、总览和实时提醒按用户设备范围过滤；没有设备管理权限时不返回设备及告警。全租户任务仅向拥有全部设备范围及相应菜单权限的用户开放。</p></ui-card>
 <ui-dialog :model-value="dialog==='user'||dialog==='role'" :title="`${editing?'编辑':'添加'}${dialog==='user'?'用户':'角色'}`" width="min(850px,94vw)" :close-on-click-modal="false" @close="dialog='';user.password=''">
  <ui-form v-if="dialog==='user'" class="user-editor" label-position="top" :disabled="saving">
   <section class="user-editor-section">
    <div class="user-editor-heading"><h3>账户信息</h3><p>用户登录时需填写所属租户、用户名和密码。</p></div>
    <div class="user-editor-grid">
     <ui-form-item label="所属租户"><ui-input :model-value="tenantId" disabled/></ui-form-item>
     <ui-form-item label="用户名" required><ui-input v-model="user.username" :disabled="editing" autocomplete="off" placeholder="3 至 64 位字母、数字、点、横线或下划线"/></ui-form-item>
     <ui-form-item label="显示名称"><ui-input v-model="user.displayName" placeholder="便于同事识别的姓名或称呼"/></ui-form-item>
     <ui-form-item v-if="!editing" label="初始密码" required><ui-input v-model="user.password" type="password" show-password autocomplete="new-password" placeholder="至少 10 位，最长 72 字节"/></ui-form-item>
     <div v-else class="user-editor-password-note">需要修改密码时，请关闭此窗口，在用户列表中选择“重置密码”。</div>
    </div>
    <div class="user-editor-switch"><span>启用账户<small>停用后用户无法登录</small></span><ui-switch v-model="user.enabled"/></div>
   </section>
   <section class="user-editor-section">
    <div class="user-editor-heading"><h3>访问范围</h3><p>分配角色即可继承功能和设备授权，支持多个角色。</p></div>
    <ui-form-item label="角色"><ui-select v-model="user.roleIds" multiple clearable placeholder="选择角色；可选择多个"><ui-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id"/></ui-select></ui-form-item>
    <div class="user-editor-heading"><h3>可查看的设备</h3><p>默认继承角色。选择其他范围会替代角色设备授权，同时限制设备、告警和助手查询。</p></div>
    <DeviceScopePicker allow-inherit :inherited-label="inheritedLabel" v-model:scope="user.deviceScope" v-model:device-ids="user.deviceIds" :devices="deviceOptions" :loading="devicesLoading" :error="devicesError" :disabled="saving" :can-view-devices="canViewDevices" @retry="loadDevices" @enable-device-menu="enableDeviceMenu" />
   </section>
   <section class="user-editor-section user-editor-permissions">
    <details><summary><span>高级：用户附加功能 <small>在角色权限基础上增加 · 已选 {{ user.permissions.length }} 项</small></span><span class="user-editor-expand">展开设置</span></summary><PermissionPicker v-model="user.permissions" :catalog="catalog"/></details>
   </section>
  </ui-form> <!-- 用户账户、范围和附加权限分别呈现。 -->
  <ui-form v-else-if="dialog==='role'" class="role-editor" label-position="top" :disabled="saving">
   <section class="role-editor-section"><h3>角色信息</h3><div class="user-editor-grid"><ui-form-item label="角色名称" required><ui-input v-model="role.name" placeholder="例如 东区值班员"/></ui-form-item><ui-form-item label="说明"><ui-input v-model="role.description" placeholder="职责或适用人员（选填）"/></ui-form-item></div></section>
   <section class="role-editor-section"><h3>可用功能</h3><PermissionPicker v-model="role.permissions" :catalog="catalog"/></section>
   <section class="role-editor-section"><h3>可查看的设备</h3><p>分配此角色并选择“继承角色”的用户，共享此设备范围。主设备与子设备分别授权。</p><DeviceScopePicker v-model:scope="role.deviceScope" v-model:device-ids="role.deviceIds" :devices="deviceOptions" :loading="devicesLoading" :error="devicesError" :disabled="saving" :can-view-devices="role.permissions.includes('menu:devices')" @retry="loadDevices" @enable-device-menu="enableDeviceMenu"/><p v-if="editing">保存后立即影响继承此角色的 {{ users.filter(u=>u.deviceScope==='inherit' && u.roleIds.includes(role.id)).length }} 个用户。</p><p>巡检、备份、告警规则等全租户功能，以及设备新增操作，还需要“全部设备”范围。</p></section>
  </ui-form>
  <template #footer><ui-button :disabled="saving" @click="dialog='';user.password=''">取消</ui-button><ui-button type="primary" :loading="saving" :disabled="deviceSelectionPending" @click="save">{{ editing ? '保存修改' : dialog==='user' ? '创建用户' : '创建角色' }}</ui-button></template>
 </ui-dialog>
 <ui-dialog :model-value="dialog==='password'" title="重置密码" width="min(460px,94vw)" @close="dialog='';password.value=''"><ui-form label-position="top"><ui-form-item :label="`用户：${password.username}`"><ui-input v-model="password.value" type="password" show-password autocomplete="new-password" placeholder="至少10位，最长72字节"/></ui-form-item></ui-form><template #footer><ui-button type="primary" :loading="saving" @click="savePassword">重置密码</ui-button></template></ui-dialog>
</template>
<style scoped>
.access-tenant { flex: 1 1 320px; margin: 0; color: var(--text-muted); font-size: var(--font-size-sm); }
.user-editor { display:grid; gap:18px; } /* 用户编辑按操作目的分区，减少连续长表单的阅读负担。 */
.user-editor-section { min-width:0; padding:18px; border:1px solid var(--border); border-radius:8px; background:var(--surface); }
.user-editor-heading { margin-bottom:15px; }
.user-editor-heading h3 { margin:0; font-size:15px; }
.user-editor-heading p { margin:4px 0 0; color:var(--text-muted); font-size:12px; line-height:1.5; }
.user-editor-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:0 16px; align-items:start; }
.user-editor-grid > * { min-width:0; }
.user-editor :deep(.n-form-item),.user-editor :deep(.n-select),.user-editor :deep(.n-input) { width:100%; min-width:0; }
.user-editor-password-note { align-self:center; padding:10px 12px; color:var(--text-muted); background:var(--surface-muted); border-radius:6px; font-size:12px; line-height:1.5; }
.user-editor-switch { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:11px 13px; background:var(--surface-muted); border-radius:6px; font-size:13px; font-weight:600; }
.user-editor-switch small { display:block; margin-top:3px; color:var(--text-muted); font-size:12px; font-weight:400; }
.user-editor-scope { display:flex; flex-wrap:wrap; gap:8px; }
.user-editor-hint { margin:4px 0 0; padding:10px 12px; color:var(--text); background:var(--surface-muted); border-radius:6px; font-size:12px; line-height:1.6; }
.user-editor-permissions { padding:0; }
.user-editor-permissions summary { display:flex; align-items:center; justify-content:space-between; gap:12px; padding:16px 18px; cursor:pointer; list-style:none; font-size:14px; font-weight:600; }
.user-editor-permissions summary::-webkit-details-marker { display:none; }
.user-editor-permissions summary small { display:block; margin-top:4px; color:var(--text-muted); font-size:12px; font-weight:400; }
.user-editor-expand { flex:none; color:var(--primary); font-size:12px; font-weight:500; }
.user-editor-permissions details[open] .user-editor-expand { font-size:0; }
.user-editor-permissions details[open] .user-editor-expand::after { content:'收起设置'; font-size:12px; }
.user-editor-permissions :deep(.permission-picker) { padding:0 18px 18px; border-top:1px solid var(--border); }
.role-editor{display:grid;gap:14px}.role-editor-section{min-width:0;padding:16px 18px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}.role-editor-section h3{margin:0;font-size:14px}.role-editor-section p{margin:5px 0 14px;color:var(--text-muted);font-size:12px;line-height:1.5}.role-editor-section :deep(.n-form-item:last-child){margin-bottom:0}
@media (max-width:640px) { .user-editor-grid { grid-template-columns:1fr; }.user-editor-section { padding:14px; }.user-editor-permissions { padding:0; }.user-editor-scope { flex-direction:column; align-items:stretch; }.user-editor-scope :deep(.n-radio-button) { width:100%; } }
</style>
