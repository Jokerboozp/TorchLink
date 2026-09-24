<script setup>
import {onMounted,reactive,ref} from 'vue' /* 引入当前代码需要的依赖。 */
import {UiMessage,UiMessageBox} from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import {api,notifyError,session} from '../api' /* 引入当前代码需要的依赖。 */
import PermissionPicker from '../components/PermissionPicker.vue' /* 引入当前代码需要的依赖。 */
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
const tab=ref('users'),users=ref([]),roles=ref([]),catalog=ref([]),loading=ref(false),saving=ref(false),dialog=ref(''),editing=ref(false) /* 声明 tab。 */
const user=reactive({username:'',displayName:'',password:'',enabled:true,roleIds:[],permissions:[],deviceScope:'none',deviceIds:[]}) /* 声明 user。 */
const role=reactive({id:'',name:'',description:'',permissions:[]}) /* 声明 role。 */
const tenantId=ref(session.tenant),deviceOptions=ref([]) /* 声明 tenantId。 */
const scopeLabel=value=>value.deviceScope==='all'?'当前租户全部设备':value.deviceScope==='selected'?`指定 ${value.deviceIds?.length||0} 台设备`:'无设备' /* 声明 scopeLabel。 */
const password=reactive({username:'',value:''}) /* 声明 password。 */
let loadVersion=0
async function load(){const version=++loadVersion;loading.value=true;try{const [u,r,p,d]=await Promise.all([api('/api/v1/access/users'),api('/api/v1/access/roles'),api('/api/v1/access/permissions'),api('/api/v1/access/device-options')]);if(version!==loadVersion)return;tenantId.value=u.tenantId;deviceOptions.value=d.items||[];users.value=u.items||[];roles.value=r.items||[];catalog.value=p.items||[]}catch(e){if(version===loadVersion)notifyError(e)}finally{if(version===loadVersion)loading.value=false}} /* 定义 load 函数。 */
function editUser(value){editing.value=!!value;Object.assign(user,{username:'',displayName:'',password:'',enabled:true,roleIds:[],permissions:[],deviceScope:'none',deviceIds:[]},value?JSON.parse(JSON.stringify(value)):{});dialog.value='user'} /* 定义 editUser 函数。 */
function editRole(value){editing.value=!!value;Object.assign(role,{id:'',name:'',description:'',permissions:[]},value?JSON.parse(JSON.stringify(value)):{});dialog.value='role'} /* 定义 editRole 函数。 */
async function save(){if(saving.value)return;saving.value=true;try{const isUser=dialog.value==='user',value=isUser?user:role;const base=isUser?'/api/v1/access/users':'/api/v1/access/roles';await api(base+(editing.value?'/'+encodeURIComponent(isUser?user.username:role.id):''),{method:editing.value?'PUT':'POST',body:JSON.stringify(value)});dialog.value='';user.password='';await load();UiMessage.success('已保存')}catch(e){notifyError(e)}finally{saving.value=false}} /* 定义 save 函数。 */
async function remove(kind,value){try{await UiMessageBox.confirm(`确认删除${kind==='users'?'用户':'角色'}“${value.displayName||value.name||value.username}”？`,'删除确认',{type:'warning'});await api(`/api/v1/access/${kind}/${encodeURIComponent(value.username||value.id)}`,{method:'DELETE'});await load()}catch(e){if(e!=='cancel'&&e!=='close')notifyError(e)}} /* 定义 remove 函数。 */
function reset(value){password.username=value.username;password.value='';dialog.value='password'} /* 定义 reset 函数。 */
async function savePassword(){saving.value=true;try{await api(`/api/v1/access/users/${encodeURIComponent(password.username)}/password`,{method:'POST',body:JSON.stringify({password:password.value})});password.value='';dialog.value='';UiMessage.success('密码已重置，旧登录已失效')}catch(e){notifyError(e)}finally{saving.value=false}} /* 定义 savePassword 函数。 */
onMounted(load) /* 执行当前语句并推进处理流程。 */
</script>
<template>
 <ui-tabs v-model="tab"><ui-tab-pane label="用户管理" name="users"/><ui-tab-pane label="角色管理" name="roles"/><ui-tab-pane label="权限说明" name="permissions"/></ui-tabs> <!-- 渲染 ui-tabs 界面元素。 -->
 <div class="page-toolbar"><ui-button v-if="tab==='users'" v-permission="'POST /api/v1/access/users'" type="primary" @click="editUser()">添加用户</ui-button><ui-button v-if="tab==='roles'" v-permission="'POST /api/v1/access/roles'" type="primary" @click="editRole()">添加角色</ui-button><ui-button :loading="loading" @click="load">刷新</ui-button></div> <!-- 渲染 div 界面元素。 -->
 <ui-alert :title="`当前租户：${tenantId}。此处创建的用户归属该租户，登录时填写相同租户。`" type="info" :closable="false" class="bottom-gap"/> <!-- 渲染 ui-alert 界面元素。 -->
 <ui-alert title="内置管理员用于系统维护，不在此列表中修改。角色权限与用户单独授权合并生效。" type="info" :closable="false"/> <!-- 渲染 ui-alert 界面元素。 -->
 <ui-table v-if="tab==='users'" v-loading="loading" :data="users" class="top-gap" empty-text="暂无用户，点击添加用户创建登录账户"><ui-table-column prop="username" label="用户名"/><ui-table-column prop="displayName" label="姓名 / 显示名称"/><ui-table-column label="所属租户"><template #default>{{tenantId}}</template></ui-table-column><ui-table-column label="设备访问范围"><template #default="{row}">{{scopeLabel(row)}}</template></ui-table-column><ui-table-column label="角色"><template #default="{row}">{{row.roleIds.map(id=>roles.find(r=>r.id===id)?.name||id).join('、')||'未分配'}}</template></ui-table-column><ui-table-column label="状态" width="100"><template #default="{row}"><ui-tag :type="row.enabled?'success':'info'">{{row.enabled?'启用':'停用'}}</ui-tag></template></ui-table-column><ui-table-column label="操作" width="270" fixed="right"><template #default="{row}"><div class="table-actions"><ui-button v-permission="'PUT /api/v1/access/users/:id'" :disabled="row.username===session.user" @click="editUser(row)">编辑</ui-button><ui-button v-permission="'POST /api/v1/access/users/:id/password'" @click="reset(row)">重置密码</ui-button><ui-button v-permission="'DELETE /api/v1/access/users/:id'" type="danger" :disabled="row.username===session.user" @click="remove('users',row)">删除</ui-button></div></template></ui-table-column></ui-table> <!-- 渲染 ui-table 界面元素。 -->
 <ui-table v-if="tab==='roles'" v-loading="loading" :data="roles" class="top-gap" empty-text="暂无角色，点击添加角色配置权限"><ui-table-column prop="name" label="角色名称"/><ui-table-column prop="id" label="角色标识"/><ui-table-column prop="description" label="说明"/><ui-table-column label="权限数量" width="100"><template #default="{row}">{{row.permissions.length}}</template></ui-table-column><ui-table-column label="操作" width="180" fixed="right"><template #default="{row}"><ui-button v-permission="'PUT /api/v1/access/roles/:id'" @click="editRole(row)">编辑</ui-button><ui-button v-permission="'DELETE /api/v1/access/roles/:id'" type="danger" @click="remove('roles',row)">删除</ui-button></template></ui-table-column></ui-table> <!-- 渲染 ui-table 界面元素。 -->
 <ui-card v-if="tab==='permissions'" class="top-gap"><h3>菜单与操作权限</h3><p>菜单权限控制可见页面及读取范围；操作权限分别控制新增、编辑、删除、下载、发布、设备控制等动作。后端会再次校验，每次请求均使用最新角色配置。</p><p>修改用户、停用用户或重置密码会使旧登录失效。角色被用户使用时不能删除。</p><p>设备列表、告警、原始报文、总览和实时提醒按用户设备范围过滤；没有设备管理权限时不返回设备及告警。全租户任务仅向拥有全部设备范围及相应菜单权限的用户开放。</p></ui-card> <!-- 渲染 ui-card 界面元素。 -->
 <ui-dialog :model-value="dialog==='user'||dialog==='role'" :title="`${editing?'编辑':'添加'}${dialog==='user'?'用户':'角色'}`" width="min(850px,94vw)" :close-on-click-modal="false" @close="dialog='';user.password=''"> <!-- 渲染 ui-dialog 界面元素。 -->
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
    <div class="user-editor-heading"><h3>访问范围</h3><p>角色决定可用功能，设备范围决定可查看的数据。</p></div>
    <ui-form-item label="角色"><ui-select v-model="user.roleIds" multiple clearable placeholder="选择角色；可选择多个"><ui-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id"/></ui-select></ui-form-item>
    <ui-form-item label="设备访问范围"><ui-radio-group v-model="user.deviceScope" class="user-editor-scope segmented-choice-group" aria-label="设备访问范围"><ui-radio-button value="none">无设备</ui-radio-button><ui-radio-button value="selected">指定设备</ui-radio-button><ui-radio-button value="all">当前租户全部设备</ui-radio-button></ui-radio-group></ui-form-item>
    <ui-form-item v-if="user.deviceScope==='selected'" label="可访问设备"><ui-select v-model="user.deviceIds" multiple filterable clearable collapse-tags collapse-tags-tooltip placeholder="选择允许查看的设备"><ui-option v-for="d in deviceOptions" :key="d.id" :label="`${d.name}（${d.id}）`" :value="d.id"/></ui-select></ui-form-item>
    <p class="user-editor-hint">查看设备需同时具有设备管理菜单权限。告警按设备范围过滤，主设备与子设备分别授权。智能助手、巡检、备份等全租户任务需要全部设备范围及对应菜单权限。</p>
   </section>
   <section class="user-editor-section user-editor-permissions">
    <details><summary><span>用户单独授权 <small>在角色权限基础上增加 · 已选 {{ user.permissions.length }} 项</small></span><span class="user-editor-expand">展开设置</span></summary><PermissionPicker v-model="user.permissions" :catalog="catalog"/></details>
   </section>
  </ui-form> <!-- 用户账户、范围和附加权限分别呈现。 -->
  <ui-form v-else class="role-editor" label-position="top" :disabled="saving"><section class="role-editor-section"><h3>角色信息</h3><p>角色标识用于系统识别，创建后不可修改。</p><div class="user-editor-grid"><ui-form-item label="角色标识" required><ui-input v-model="role.id" :disabled="editing" placeholder="例如 operations"/></ui-form-item><ui-form-item label="角色名称" required><ui-input v-model="role.name" placeholder="例如 设备运维"/></ui-form-item></div><ui-form-item label="说明"><ui-input v-model="role.description"/></ui-form-item></section><section class="role-editor-section"><h3>菜单与操作权限</h3><p>勾选该角色可访问的菜单和可执行的操作。</p><PermissionPicker v-model="role.permissions" :catalog="catalog"/></section></ui-form> <!-- 角色身份与权限分开呈现。 -->
  <template #footer><ui-button :disabled="saving" @click="dialog='';user.password=''">取消</ui-button><ui-button type="primary" :loading="saving" @click="save">{{ editing ? '保存修改' : dialog==='user' ? '创建用户' : '创建角色' }}</ui-button></template>
 </ui-dialog> <!-- 结束当前界面区域。 -->
 <ui-dialog :model-value="dialog==='password'" title="重置密码" width="min(460px,94vw)" @close="dialog='';password.value=''"><ui-form label-position="top"><ui-form-item :label="`用户：${password.username}`"><ui-input v-model="password.value" type="password" show-password autocomplete="new-password" placeholder="至少10位，最长72字节"/></ui-form-item></ui-form><template #footer><ui-button type="primary" :loading="saving" @click="savePassword">重置密码</ui-button></template></ui-dialog> <!-- 渲染 ui-dialog 界面元素。 -->
</template>
<style scoped>
.user-editor { display:grid; gap:18px; } /* 用户编辑按操作目的分区，减少连续长表单的阅读负担。 */
.user-editor-section { min-width:0; padding:18px; border:1px solid var(--border); border-radius:8px; background:var(--card); }
.user-editor-heading { margin-bottom:15px; }
.user-editor-heading h3 { margin:0; font-size:15px; }
.user-editor-heading p { margin:4px 0 0; color:var(--muted-foreground); font-size:12px; line-height:1.5; }
.user-editor-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:0 16px; }
.user-editor-grid > * { min-width:0; }
.user-editor :deep(.n-form-item),.user-editor :deep(.n-select),.user-editor :deep(.n-input) { width:100%; min-width:0; }
.user-editor-password-note { align-self:center; padding:10px 12px; color:var(--muted-foreground); background:var(--surface-subtle); border-radius:6px; font-size:12px; line-height:1.5; }
.user-editor-switch { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:11px 13px; background:var(--surface-subtle); border-radius:6px; font-size:13px; font-weight:600; }
.user-editor-switch small { display:block; margin-top:3px; color:var(--muted-foreground); font-size:12px; font-weight:400; }
.user-editor-scope { display:flex; flex-wrap:wrap; gap:8px; }
.user-editor-hint { margin:4px 0 0; padding:10px 12px; color:var(--accent-foreground); background:var(--surface-subtle); border-radius:6px; font-size:12px; line-height:1.6; }
.user-editor-permissions { padding:0; }
.user-editor-permissions summary { display:flex; align-items:center; justify-content:space-between; gap:12px; padding:16px 18px; cursor:pointer; list-style:none; font-size:14px; font-weight:600; }
.user-editor-permissions summary::-webkit-details-marker { display:none; }
.user-editor-permissions summary small { display:block; margin-top:4px; color:var(--muted-foreground); font-size:12px; font-weight:400; }
.user-editor-expand { flex:none; color:var(--primary); font-size:12px; font-weight:500; }
.user-editor-permissions details[open] .user-editor-expand { font-size:0; }
.user-editor-permissions details[open] .user-editor-expand::after { content:'收起设置'; font-size:12px; }
.user-editor-permissions :deep(.permission-picker) { padding:0 18px 18px; border-top:1px solid var(--border); }
.role-editor{display:grid;gap:14px}.role-editor-section{min-width:0;padding:16px 18px;border:1px solid var(--border);border-radius:8px;background:var(--card)}.role-editor-section h3{margin:0;font-size:14px}.role-editor-section p{margin:5px 0 14px;color:var(--muted-foreground);font-size:12px;line-height:1.5}.role-editor-section :deep(.n-form-item:last-child){margin-bottom:0}
@media (max-width:640px) { .user-editor-grid { grid-template-columns:1fr; }.user-editor-section { padding:14px; }.user-editor-permissions { padding:0; }.user-editor-scope { flex-direction:column; align-items:stretch; }.user-editor-scope :deep(.n-radio-button) { width:100%; } }
</style>
