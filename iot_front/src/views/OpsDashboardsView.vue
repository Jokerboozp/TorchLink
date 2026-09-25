<script setup>
// 仪表盘中心：仪表盘、文件夹与数据源保存在 Grafana，平台原生渲染与编辑；收藏按账户保存在平台。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Folder, FolderPlus, Import, LayoutGrid, Pencil, Plus, Star, Trash2 } from '@lucide/vue'
import { can } from '../permissions'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { emptyDashboard } from '../ops/dashboard.js'
import { latest, opsErrorText, opsGet, opsSend, takeNavigation } from '../ops/opsApi.js'
import DashboardImport from '../components/ops/DashboardImport.vue'
import DashboardViewer from '../components/ops/DashboardViewer.vue'
import DataSourcesPanel from '../components/ops/DataSourcesPanel.vue'

const emit = defineEmits(['navigate'])
const tab = ref('dashboards')
const items = ref([])
const folders = ref([])
const dataSources = ref([])
const loading = ref(false)
const error = ref('')
const search = ref('')
const folderFilter = ref('')
const tagFilter = ref('')
const favoritesOnly = ref(false)
const openUid = ref('')
const creating = ref(null)
const importVisible = ref(false)
const runner = latest()
let searchTimer = null
const canCreate = computed(() => can('POST /api/v1/ops/dashboards'))
const canImport = computed(() => can('POST /api/v1/ops/dashboards/import'))
const canFolderCreate = computed(() => can('POST /api/v1/ops/folders'))
const canFolderEdit = computed(() => can('PUT /api/v1/ops/folders/:uid'))
const canFolderDelete = computed(() => can('DELETE /api/v1/ops/folders/:uid'))
const tags = computed(() => [...new Set(items.value.flatMap(item => item.tags || []))].sort())
const viewing = computed(() => Boolean(openUid.value || creating.value))

// 文件夹按层级排列，子文件夹缩进显示。
const folderTree = computed(() => {
  const byParent = {}
  for (const f of folders.value) (byParent[f.parentUid || ''] ||= []).push(f)
  const out = []
  const walk = (parent, depth) => { for (const f of (byParent[parent] || []).sort((a, b) => a.title.localeCompare(b.title, 'zh-CN'))) { out.push({ ...f, depth }); walk(f.uid, depth + 1) } }
  walk('', 0)
  const known = new Set(out.map(f => f.uid))
  for (const f of folders.value) if (!known.has(f.uid)) out.push({ ...f, depth: 0 })
  return out
})

async function loadList() {
  loading.value = true
  try {
    items.value = (await runner.run(signal => opsGet('/api/v1/ops/dashboards', { query: search.value.trim(), folderUid: folderFilter.value, tag: tagFilter.value, favorites: favoritesOnly.value || undefined }, signal))).items || []
    error.value = ''
  } catch (e) { if (e?.name !== 'AbortError') error.value = opsErrorText(e) } finally { loading.value = false }
}
async function loadMeta() {
  const [f, d] = await Promise.allSettled([opsGet('/api/v1/ops/folders'), opsGet('/api/v1/ops/datasources')])
  folders.value = f.status === 'fulfilled' ? f.value.items || [] : []
  dataSources.value = d.status === 'fulfilled' ? d.value.items || [] : []
}
watch(search, () => { clearTimeout(searchTimer); searchTimer = setTimeout(loadList, 300) })
watch([folderFilter, tagFilter, favoritesOnly], loadList)

function open(uid) { openUid.value = uid; creating.value = null }
function create() { creating.value = emptyDashboard(); openUid.value = '' }
function closeViewer() { openUid.value = ''; creating.value = null; loadList() }
function saved(uid) { creating.value = null; openUid.value = uid; loadList() }
function deleted() { closeViewer() }
async function favorite(item) {
  try {
    await opsSend(item.favorite ? 'DELETE' : 'PUT', `/api/v1/ops/preferences/favorites/${encodeURIComponent(item.uid)}`)
    item.favorite = !item.favorite
    if (favoritesOnly.value && !item.favorite) items.value = items.value.filter(i => i !== item)
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}
async function removeDashboard(item) {
  try { await UiMessageBox.confirm(`删除仪表盘“${item.title}”？Grafana 中的仪表盘也会被删除。`, '删除仪表盘', { confirmButtonText: '删除' }) } catch { return }
  try {
    await opsSend('DELETE', `/api/v1/ops/dashboards/${encodeURIComponent(item.uid)}`)
    UiMessage.success('仪表盘已删除')
    loadList()
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}

// 文件夹
const folderDialog = ref({ visible: false, uid: '', title: '' })
const folderSaving = ref(false)
function editFolder(folder) { folderDialog.value = { visible: true, uid: folder?.uid || '', title: folder?.title || '' } }
async function saveFolder() {
  const { uid, title } = folderDialog.value
  folderSaving.value = true
  try {
    if (uid) await opsSend('PUT', `/api/v1/ops/folders/${encodeURIComponent(uid)}`, { title })
    else await opsSend('POST', '/api/v1/ops/folders', { title })
    folderDialog.value.visible = false
    UiMessage.success('文件夹已保存')
    await loadMeta()
    loadList()
  } catch (e) { UiMessage.error(opsErrorText(e)) } finally { folderSaving.value = false }
}
async function removeFolder(folder) {
  try { await UiMessageBox.confirm(`删除空文件夹“${folder.title}”？文件夹中还有仪表盘时不会删除。`, '删除文件夹') } catch { return }
  try {
    await opsSend('DELETE', `/api/v1/ops/folders/${encodeURIComponent(folder.uid)}`)
    if (folderFilter.value === folder.uid) folderFilter.value = ''
    await loadMeta()
    UiMessage.success('文件夹已删除')
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}

onMounted(async () => {
  const nav = takeNavigation()
  if (nav?.tab) tab.value = nav.tab
  if (nav?.uid) openUid.value = nav.uid
  await loadMeta()
  loadList()
})
onBeforeUnmount(() => { runner.cancel(); clearTimeout(searchTimer) })
</script>

<template>
  <div class="ops-page">
    <ui-tabs v-model="tab">
      <ui-tab-pane name="dashboards" label="仪表盘">
        <DashboardViewer v-if="viewing" :uid="openUid" :initial="creating" :folder-uid="folderFilter" :folders="folders" :data-sources="dataSources" @close="closeViewer" @saved="saved" @deleted="deleted" @navigate="(page, detail) => emit('navigate', page, detail)" />
        <div v-else class="dash-list">
          <aside class="folders" aria-label="文件夹">
            <button type="button" class="folder-item" :class="{ 'is-active': !folderFilter && !favoritesOnly }" @click="folderFilter = ''; favoritesOnly = false"><LayoutGrid />全部仪表盘</button>
            <button type="button" class="folder-item" :class="{ 'is-active': favoritesOnly }" @click="favoritesOnly = !favoritesOnly"><Star />我的收藏</button>
            <div class="folders__head"><span>文件夹</span><ui-button v-if="canFolderCreate" text size="small" aria-label="新建文件夹" @click="editFolder(null)"><FolderPlus /></ui-button></div>
            <div v-for="f in folderTree" :key="f.uid" class="folder-row" :class="{ 'is-active': folderFilter === f.uid }" :style="{ paddingLeft: `${8 + f.depth * 14}px` }">
              <button type="button" class="folder-item" @click="folderFilter = f.uid; favoritesOnly = false"><Folder />{{ f.title }}</button>
              <span class="folder-row__actions">
                <ui-button v-if="canFolderEdit && f.canEdit !== false" text size="small" aria-label="重命名文件夹" @click="editFolder(f)"><Pencil /></ui-button>
                <ui-button v-if="canFolderDelete && f.canEdit !== false" text size="small" aria-label="删除文件夹" @click="removeFolder(f)"><Trash2 /></ui-button>
              </span>
            </div>
            <p v-if="!folderTree.length" class="muted">暂无文件夹</p>
          </aside>
          <section class="dash-list__main">
            <div class="list-toolbar">
              <ui-input v-model="search" size="small" clearable placeholder="搜索仪表盘标题" aria-label="搜索仪表盘" class="list-toolbar__search" />
              <ui-select v-model="tagFilter" size="small" clearable placeholder="全部标签" aria-label="按标签筛选" class="list-toolbar__tag"><ui-option v-for="tag in tags" :key="tag" :value="tag" :label="tag" /></ui-select>
              <div class="list-toolbar__actions">
                <ui-button v-if="canImport" size="small" @click="importVisible = true"><Import />导入</ui-button>
                <ui-button v-if="canCreate" size="small" type="primary" @click="create"><Plus />新建仪表盘</ui-button>
              </div>
            </div>
            <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
            <ui-table :data="items" size="small" row-key="uid" :empty-text="loading ? '正在读取…' : favoritesOnly ? '还没有收藏的仪表盘' : '没有仪表盘，可以新建或从模板导入'">
              <ui-table-column label="" width="46"><template #default="{ row }"><button type="button" class="star" :class="{ 'is-on': row.favorite }" :aria-label="row.favorite ? '取消收藏' : '收藏'" @click="favorite(row)"><Star /></button></template></ui-table-column>
              <ui-table-column label="标题" min-width="240"><template #default="{ row }"><button type="button" class="title-link" @click="open(row.uid)">{{ row.title }}</button></template></ui-table-column>
              <ui-table-column label="文件夹" min-width="140"><template #default="{ row }">{{ row.folderTitle || '根目录' }}</template></ui-table-column>
              <ui-table-column label="标签" min-width="180"><template #default="{ row }"><ui-tag v-for="tag in row.tags" :key="tag" size="small" class="tag-gap">{{ tag }}</ui-tag></template></ui-table-column>
              <ui-table-column label="操作" width="120"><template #default="{ row }"><div class="table-actions table-actions--start"><ui-button text size="small" @click="open(row.uid)">打开</ui-button><ui-button v-if="can('DELETE /api/v1/ops/dashboards/:uid')" text size="small" type="danger" @click="removeDashboard(row)">删除</ui-button></div></template></ui-table-column>
            </ui-table>
          </section>
        </div>
      </ui-tab-pane>
      <ui-tab-pane name="datasources" label="数据源">
        <DataSourcesPanel v-if="tab === 'datasources'" @changed="loadMeta" />
      </ui-tab-pane>
    </ui-tabs>

    <DashboardImport v-model="importVisible" :folders="folders" :data-sources="dataSources" :folder-uid="folderFilter" @imported="uid => { loadList(); if (uid) open(uid) }" />
    <ui-dialog v-model="folderDialog.visible" :title="folderDialog.uid ? '重命名文件夹' : '新建文件夹'" width="min(420px, 94vw)">
      <label class="folder-field">名称<ui-input v-model="folderDialog.title" maxlength="100" @keydown.enter="saveFolder" /></label>
      <template #footer><ui-button @click="folderDialog.visible = false">取消</ui-button><ui-button type="primary" :loading="folderSaving" :disabled="!folderDialog.title.trim()" @click="saveFolder">保存</ui-button></template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.ops-page { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
.dash-list { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
.folders { display: grid; align-content: start; gap: 2px; min-width: 0; }
.folders__head { display: flex; align-items: center; justify-content: space-between; margin-top: var(--space-3); padding: 0 var(--space-2); color: var(--text-muted); font-size: var(--font-size-xs); }
.folder-item { display: flex; align-items: center; gap: var(--space-2); width: 100%; min-width: 0; padding: 6px var(--space-2); overflow: hidden; color: var(--text); font-size: var(--font-size-sm); text-align: left; text-overflow: ellipsis; white-space: nowrap; background: none; border: 0; border-radius: var(--radius-sm); cursor: pointer; }
.folder-item svg { flex: none; width: 15px; height: 15px; color: var(--text-muted); }
.folder-item:hover, .folder-row:hover { background: var(--surface-hover); }
.folder-item.is-active, .folder-row.is-active { background: var(--primary-soft); }
.folder-row { display: flex; align-items: center; border-radius: var(--radius-sm); }
.folder-row .folder-item { padding-left: 0; background: none; }
.folder-row__actions { display: none; }
.folder-row:hover .folder-row__actions, .folder-row:focus-within .folder-row__actions { display: inline-flex; }
.dash-list__main { display: grid; grid-template-columns: minmax(0, 1fr); align-content: start; gap: var(--space-3); min-width: 0; }
.list-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
.list-toolbar__search { width: 260px; }
.list-toolbar__tag { width: 160px; }
.list-toolbar__actions { display: flex; gap: var(--space-2); margin-left: auto; }
.star { display: inline-grid; place-items: center; width: 24px; height: 24px; padding: 0; color: var(--text-muted); background: none; border: 0; cursor: pointer; }
.star svg { width: 15px; height: 15px; }
.star.is-on { color: var(--warning); }
.star.is-on svg { fill: currentColor; }
.title-link { padding: 0; color: var(--primary-text); font-size: var(--font-size-sm); text-align: left; background: none; border: 0; cursor: pointer; }
.title-link:hover { text-decoration: underline; }
.tag-gap { margin-right: 4px; }
.muted { margin: 0; padding: 0 var(--space-2); color: var(--text-muted); font-size: var(--font-size-xs); }
.folder-field { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
@media (max-width: 900px) {
  .dash-list { grid-template-columns: minmax(0, 1fr); }
  .folders { grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); }
  .folders__head { grid-column: 1 / -1; }
  .list-toolbar__search, .list-toolbar__tag { flex: 1 1 160px; width: auto; }
  .folder-row__actions { display: inline-flex; }
}
</style>
