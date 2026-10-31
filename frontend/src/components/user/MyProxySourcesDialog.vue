<template>
  <BaseDialog
    :show="show"
    :title="t('admin.proxies.sourceManagerTitle')"
    width="extra-wide"
    :close-on-escape="!busy && !editorOpen && !pendingDelete"
    :show-close-button="!busy"
    @close="closeManager"
  >
    <div data-test="user-proxy-source-manager" :aria-busy="loading || busy">
      <p
        v-if="errorMessage"
        role="alert"
        class="mb-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300"
      >
        {{ errorMessage }}
      </p>
      <section class="min-w-0">
        <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.proxies.sourceManager') }}
          </h4>
          <div class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              class="btn btn-primary btn-sm"
              data-test="user-proxy-source-add"
              :disabled="controlsDisabled"
              @click="openEditor()"
            >
              <Icon name="plus" size="sm" class="mr-2" />
              {{ mr('actions.addSource') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              data-test="user-proxy-source-sync-all"
              :disabled="controlsDisabled || sources.length === 0"
              @click="syncAllSources"
            >
              <Icon name="refresh" size="sm" class="mr-2" :class="syncingAll ? 'animate-spin' : ''" />
              {{ mr('actions.syncAllSources') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              data-test="user-proxy-source-refresh"
              :disabled="controlsDisabled"
              :title="t('common.refresh')"
              :aria-label="t('common.refresh')"
              @click="loadSources()"
            >
              <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
        <div
          v-if="syncAllSummary"
          class="mb-3 rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-xs text-gray-600 dark:border-dark-600 dark:bg-dark-700/40 dark:text-gray-300"
          data-test="user-proxy-source-sync-all-summary"
          role="status"
        >
          {{ syncAllSummary }}
        </div>
        <div
          class="proxy-source-table flex min-h-0 min-w-0 flex-col overflow-hidden rounded-md border border-gray-200 dark:border-dark-600"
          data-test="user-proxy-source-table-scroll"
        >
          <DataTable :columns="columns" :data="sources" :loading="loading" :expandable-actions="false" row-key="id">
            <template #cell-name="{ row: source }">
              <div class="min-w-0 max-w-[260px] whitespace-normal break-words font-medium text-gray-900 dark:text-white">
                {{ source.name }}
              </div>
              <div
                class="mt-1 max-w-[260px] truncate font-mono text-xs text-gray-500 dark:text-gray-400"
                :title="maskSubscriptionUrl(source.subscription_url)"
              >
                {{ maskSubscriptionUrl(source.subscription_url) }}
              </div>
            </template>
            <template #cell-visibility="{ row: source }">
              <span :class="['badge', source.is_public ? 'badge-success' : 'badge-gray']">
                {{ mr(source.is_public ? 'states.public' : 'states.private') }}
              </span>
            </template>
            <template #cell-nodes="{ row: source }">
              <button
                type="button"
                class="text-primary-600 hover:underline disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400"
                :data-test="`user-proxy-source-filter-${source.id}`"
                :disabled="controlsDisabled"
                :title="mr('sources.filterBySource')"
                @click="filterBySource(source)"
              >
                {{ mr('sources.nodeCount', { active: Number(source.active_node_count) || 0, total: Number(source.node_count) || 0 }) }}
              </button>
            </template>
            <template #cell-interval="{ row: source }">
              {{ t('admin.proxies.sourceIntervalValue', { minutes: source.refresh_interval_minutes }) }}
              <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ source.sync_enabled === false
                  ? mr('sources.syncPaused')
                  : mr('sources.nextSyncAt', { time: source.next_sync_at ? formatDateTime(source.next_sync_at) : '-' }) }}
              </div>
            </template>
            <template #cell-quota="{ row: source }">
              <div>{{ trafficText(source) }}</div>
              <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ mr('sources.expiresAt', { time: source.sub_expires_at ? formatDateTime(source.sub_expires_at) : '-' }) }}
              </div>
            </template>
            <template #cell-status="{ row: source }">
              <span :class="['badge', syncStatus(source.last_sync_status).class]">
                {{ t(syncStatus(source.last_sync_status).label) }}
              </span>
              <div class="mt-1 whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">
                {{ source.last_synced_at ? formatDateTime(source.last_synced_at) : '-' }}
              </div>
              <div
                v-if="source.last_sync_error"
                class="mt-1 max-w-[220px] truncate text-xs text-red-600 dark:text-red-400"
                :title="source.last_sync_error"
              >
                {{ source.last_sync_error }}
              </div>
            </template>
            <template #cell-actions="{ row: source }">
              <div class="flex justify-end gap-1.5">
                <button
                  type="button"
                  class="btn btn-secondary btn-sm !px-2"
                  :data-test="`user-proxy-source-sync-${source.id}`"
                  :disabled="controlsDisabled"
                  :title="t('admin.proxies.sourceSync')"
                  :aria-label="t('admin.proxies.sourceSync')"
                  @click="syncSource(source)"
                >
                  <Icon name="refresh" size="sm" :class="syncingId === source.id ? 'animate-spin' : ''" />
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm !px-2"
                  :data-test="`user-proxy-source-toggle-${source.id}`"
                  :disabled="controlsDisabled"
                  :title="mr(source.sync_enabled === false ? 'actions.resumeSync' : 'actions.pauseSync')"
                  :aria-label="mr(source.sync_enabled === false ? 'actions.resumeSync' : 'actions.pauseSync')"
                  @click="toggleSync(source)"
                >
                  <Icon :name="togglingId === source.id ? 'refresh' : source.sync_enabled === false ? 'play' : 'ban'" size="sm" :class="togglingId === source.id ? 'animate-spin' : ''" />
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm !px-2"
                  :data-test="`user-proxy-source-edit-${source.id}`"
                  :disabled="controlsDisabled"
                  :title="t('common.edit')"
                  :aria-label="t('common.edit')"
                  @click="openEditor(source)"
                >
                  <Icon name="edit" size="sm" />
                </button>
                <button
                  type="button"
                  class="btn btn-danger btn-sm !px-2"
                  :data-test="`user-proxy-source-delete-${source.id}`"
                  :disabled="controlsDisabled"
                  :title="t('common.delete')"
                  :aria-label="t('common.delete')"
                  @click="requestDelete(source)"
                >
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </template>
            <template #empty>
              <p class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ mr('table.noSources') }}</p>
            </template>
          </DataTable>
        </div>
        <fieldset v-if="pagination.total > PAGE_SIZE" :disabled="controlsDisabled" class="min-w-0">
          <Pagination
            :total="pagination.total"
            :page="pagination.page"
            :page-size="PAGE_SIZE"
            :show-page-size-selector="false"
            data-test="user-proxy-source-pagination"
            @update:page="changePage"
          />
        </fieldset>
      </section>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" data-test="user-proxy-source-close" :disabled="busy" @click="closeManager">
        {{ t('common.close') }}
      </button>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="editorOpen"
    :title="mr(editingId ? 'actions.editSource' : 'actions.addSource')"
    width="normal"
    :z-index="60"
    :close-on-escape="!saving"
    :show-close-button="!saving"
    @close="closeEditor"
  >
    <p v-if="editorError" role="alert" class="mb-5 rounded-md bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
      {{ editorError }}
    </p>
    <form id="user-proxy-source-form" class="space-y-5" @submit.prevent="saveSource">
      <fieldset :disabled="saving" class="space-y-5">
        <div>
          <label for="user-proxy-source-name" class="input-label">{{ mr('fields.name') }}</label>
          <input id="user-proxy-source-name" v-model.trim="form.name" type="text" required class="input" :placeholder="t('admin.proxies.subscriptionNamePlaceholder')" />
        </div>
        <div>
          <label for="user-proxy-source-url" class="input-label">{{ mr('fields.subscriptionUrl') }}</label>
          <input id="user-proxy-source-url" v-model.trim="form.subscription_url" type="url" required class="input" :placeholder="t('admin.proxies.subscriptionUrlPlaceholder')" />
        </div>
        <div>
          <label for="user-proxy-source-interval" class="input-label">{{ mr('fields.refreshIntervalMinutes') }}</label>
          <input id="user-proxy-source-interval" v-model.number="form.refresh_interval_minutes" type="number" min="5" max="10080" required class="input" />
          <p class="input-hint mt-2">{{ t('admin.proxies.refreshIntervalHint') }}</p>
        </div>
        <label class="flex cursor-pointer items-center gap-2">
          <input v-model="form.sync_enabled" type="checkbox" data-test="user-proxy-source-sync-enabled" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
          <span class="text-sm text-gray-700 dark:text-gray-200">{{ mr('fields.autoSyncEnabled') }}</span>
        </label>
        <label class="flex cursor-pointer items-center gap-2">
          <input v-model="form.is_public" type="checkbox" data-test="user-proxy-source-is-public" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
          <span class="text-sm text-gray-700 dark:text-gray-200">{{ mr('fields.publicForAllUsers') }}</span>
        </label>
      </fieldset>
    </form>
    <template #footer>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" data-test="user-proxy-source-editor-cancel" :disabled="saving" @click="closeEditor">
          {{ t('common.cancel') }}
        </button>
        <button type="submit" form="user-proxy-source-form" class="btn btn-primary" data-test="user-proxy-source-editor-save" :disabled="saving || !formReady">
          {{ saving ? t('admin.proxies.saving') : mr(editingId ? 'actions.updateSource' : 'actions.saveSource') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="Boolean(pendingDelete)"
    :title="t('admin.proxies.sourceDeleteTitle')"
    width="narrow"
    :z-index="60"
    :close-on-escape="!deleting"
    :show-close-button="!deleting"
    @close="cancelDelete"
  >
    <p class="text-sm text-gray-600 dark:text-gray-400">
      {{ t('admin.proxies.sourceDeleteConfirm', { name: pendingDelete?.name || '' }) }}
    </p>
    <p v-if="deleteError" role="alert" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
      {{ deleteError }}
    </p>
    <template #footer>
      <button type="button" class="btn btn-secondary" data-test="user-proxy-source-delete-cancel" :disabled="deleting" @click="cancelDelete">{{ t('common.cancel') }}</button>
      <button type="button" class="btn btn-danger" data-test="user-proxy-source-delete-confirm" :disabled="deleting" @click="confirmDelete">
        <Icon v-if="deleting" name="refresh" size="sm" class="mr-2 animate-spin" />
        {{ t('common.delete') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { myResourcesApi, type ResourceItem, type ResourcePage } from '@/api/myResources'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import type { Column } from '@/components/common/types'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import type { ProxySourceSyncAllResult } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatBytes, formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{
  close: []
  changed: []
  loaded: [page: ResourcePage]
  deleted: [id: number]
  filter: [source: ResourceItem]
}>()
const { t } = useI18n()
const mr = (key: string, params?: Record<string, unknown>) => t(`myResources.${key}`, params || {})
const appStore = useAppStore()
const PAGE_SIZE = 100
const sources = ref<ResourceItem[]>([])
const pagination = reactive({ page: 1, total: 0, pages: 1 })
const loading = ref(false)
const errorMessage = ref('')
const editorOpen = ref(false)
const editorError = ref('')
const editingId = ref<number | null>(null)
const saving = ref(false)
const syncingId = ref<number | null>(null)
const togglingId = ref<number | null>(null)
const syncingAll = ref(false)
const syncAllResult = ref<ProxySourceSyncAllResult | null>(null)
const pendingDelete = ref<ResourceItem | null>(null)
const deleting = ref(false)
const deleteError = ref('')
const busy = computed(() => saving.value || syncingAll.value || syncingId.value !== null || togglingId.value !== null || deleting.value)
const controlsDisabled = computed(() => loading.value || busy.value || editorOpen.value || pendingDelete.value !== null)
const form = reactive({ name: '', subscription_url: '', refresh_interval_minutes: 1440, is_public: false, sync_enabled: true })
const formReady = computed(() => {
  const interval = Number(form.refresh_interval_minutes)
  if (!form.name.trim() || !Number.isInteger(interval) || interval < 5 || interval > 10080) return false
  try {
    const url = new URL(form.subscription_url.trim())
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
})
const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.proxies.sourceColumnName'), class: 'min-w-[220px] max-w-[260px]' },
  { key: 'visibility', label: t('admin.proxies.sourceColumnVisibility'), class: 'min-w-[96px] text-center' },
  { key: 'nodes', label: t('admin.proxies.sourceColumnNodes'), class: 'min-w-[96px] text-center' },
  { key: 'interval', label: t('admin.proxies.sourceColumnInterval'), class: 'min-w-[180px]' },
  { key: 'quota', label: t('admin.proxies.sourceColumnQuota'), class: 'min-w-[180px]' },
  { key: 'status', label: t('admin.proxies.sourceColumnStatus'), class: 'min-w-[180px]' },
  { key: 'actions', label: t('admin.proxies.sourceColumnActions'), class: 'min-w-[156px] text-right' },
])
const syncAllSummary = computed(() => {
  const result = syncAllResult.value
  if (!result) return ''
  return mr('sources.syncAllSummary', {
    total: Number(result.total) || 0,
    success: Number(result.success_count) || 0,
    partial: Number(result.partial_count) || 0,
    failed: Number(result.failed_count) || 0,
    skipped: Number(result.skipped_count) || 0,
    deferred: Number(result.deferred_count) || 0,
    created: Number(result.created_count) || 0,
    updated: Number(result.updated_count) || 0,
  })
})

let loadRequest = 0

async function loadSources(page = pagination.page): Promise<void> {
  if (!props.show) return
  const request = ++loadRequest
  loading.value = true
  errorMessage.value = ''
  try {
    let response = await myResourcesApi.proxies.sources.list({ page, page_size: PAGE_SIZE })
    let pages = Math.max(1, Math.ceil((Number(response.total) || 0) / PAGE_SIZE))
    if (request !== loadRequest || !props.show) return
    if (page > pages) {
      page = pages
      response = await myResourcesApi.proxies.sources.list({ page, page_size: PAGE_SIZE })
      pages = Math.max(1, Math.ceil((Number(response.total) || 0) / PAGE_SIZE))
    }
    if (request !== loadRequest || !props.show) return
    sources.value = response.items || []
    Object.assign(pagination, { page, pages, total: Math.max(0, Number(response.total) || 0) })
    emit('loaded', { items: sources.value, total: pagination.total, page, pages, page_size: PAGE_SIZE })
  } catch (error) {
    if (request === loadRequest && props.show) errorMessage.value = extractApiErrorMessage(error, mr('messages.sourceLoadFailed'))
  } finally {
    if (request === loadRequest) loading.value = false
  }
}

function changePage(page: number): void {
  if (controlsDisabled.value) return
  const nextPage = Math.min(Math.max(1, page), pagination.pages)
  if (nextPage !== pagination.page) void loadSources(nextPage)
}

function closeManager(): void {
  if (busy.value || editorOpen.value || pendingDelete.value) return
  emit('close')
}

function filterBySource(source: ResourceItem): void {
  if (controlsDisabled.value) return
  emit('filter', source)
  emit('close')
}

function resetForm(): void {
  editingId.value = null
  editorError.value = ''
  Object.assign(form, { name: '', subscription_url: '', refresh_interval_minutes: 1440, is_public: false, sync_enabled: true })
}

function openEditor(source?: ResourceItem): void {
  if (controlsDisabled.value) return
  resetForm()
  if (source) {
    editingId.value = Number(source.id)
    Object.assign(form, {
      name: String(source.name || ''),
      subscription_url: String(source.subscription_url || ''),
      refresh_interval_minutes: Number(source.refresh_interval_minutes) || 1440,
      is_public: Boolean(source.is_public),
      sync_enabled: source.sync_enabled !== false,
    })
  }
  editorOpen.value = true
}

function closeEditor(): void {
  if (saving.value) return
  editorOpen.value = false
  resetForm()
}

async function saveSource(): Promise<void> {
  if (!editorOpen.value || busy.value || !formReady.value) return
  saving.value = true
  editorError.value = ''
  try {
    const payload = { ...form, name: form.name.trim(), subscription_url: form.subscription_url.trim(), refresh_interval_minutes: Number(form.refresh_interval_minutes) }
    if (editingId.value) await myResourcesApi.proxies.sources.update(editingId.value, payload)
    else await myResourcesApi.proxies.sources.create(payload)
    appStore.showSuccess(mr('messages.sourceSaved'))
    editorOpen.value = false
    resetForm()
    emit('changed')
    await loadSources()
  } catch (error) {
    editorError.value = extractApiErrorMessage(error, mr('messages.sourceSaveFailed'))
  } finally {
    saving.value = false
  }
}

async function toggleSync(source: ResourceItem): Promise<void> {
  if (controlsDisabled.value) return
  togglingId.value = Number(source.id)
  errorMessage.value = ''
  const enabled = source.sync_enabled === false
  try {
    // The owner-scoped update replaces source fields, so preserve its visibility and URL.
    await myResourcesApi.proxies.sources.update(Number(source.id), {
      name: String(source.name || ''),
      subscription_url: String(source.subscription_url || ''),
      refresh_interval_minutes: Number(source.refresh_interval_minutes) || 1440,
      is_public: Boolean(source.is_public),
      sync_enabled: enabled,
    })
    appStore.showSuccess(mr(enabled ? 'messages.sourceSyncResumed' : 'messages.sourceSyncPaused'))
    await loadSources()
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error, mr('messages.sourceSaveFailed'))
  } finally {
    togglingId.value = null
  }
}

async function syncSource(source: ResourceItem): Promise<void> {
  if (controlsDisabled.value) return
  syncingId.value = Number(source.id)
  errorMessage.value = ''
  try {
    const result = await myResourcesApi.proxies.sources.sync(Number(source.id))
    const count = Number(result?.imported_count) || (Number(result?.created_count) || 0) + (Number(result?.updated_count) || 0)
    const errors = Array.isArray(result?.errors) ? result.errors.length : 0
    if (errors > 0) appStore.showWarning(t('admin.proxies.sourceSyncPartial', { count, errors }))
    else appStore.showSuccess(mr('messages.sourceSynced', { count }))
    emit('changed')
    await loadSources()
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error, mr('messages.sourceSyncFailed'))
  } finally {
    syncingId.value = null
  }
}

async function syncAllSources(): Promise<void> {
  if (controlsDisabled.value || sources.value.length === 0) return
  syncingAll.value = true
  errorMessage.value = ''
  try {
    const result = await myResourcesApi.proxies.sources.syncAll()
    syncAllResult.value = result
    const failed = Number(result.failed_count) || 0
    if (failed > 0) appStore.showWarning(mr('messages.sourceSyncAllPartial', { failed }))
    else appStore.showSuccess(mr('messages.sourceSyncAllDone'))
    emit('changed')
    await loadSources()
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error, mr('messages.sourceSyncFailed'))
  } finally {
    syncingAll.value = false
  }
}

function requestDelete(source: ResourceItem): void {
  if (controlsDisabled.value) return
  deleteError.value = ''
  pendingDelete.value = source
}

function cancelDelete(): void {
  if (deleting.value) return
  pendingDelete.value = null
  deleteError.value = ''
}

async function confirmDelete(): Promise<void> {
  if (!pendingDelete.value || busy.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    const id = Number(pendingDelete.value.id)
    await myResourcesApi.proxies.sources.delete(id)
    pendingDelete.value = null
    sources.value = sources.value.filter(source => Number(source.id) !== id)
    pagination.total = Math.max(0, pagination.total - 1)
    pagination.pages = Math.max(1, Math.ceil(pagination.total / PAGE_SIZE))
    pagination.page = Math.min(pagination.page, pagination.pages)
    appStore.showSuccess(mr('messages.sourceDeleted'))
    emit('deleted', id)
    await loadSources()
  } catch (error) {
    deleteError.value = extractApiErrorMessage(error, mr('messages.sourceDeleteFailed'))
  } finally {
    deleting.value = false
  }
}

function maskSubscriptionUrl(value: unknown): string {
  try {
    const url = new URL(String(value || '').trim())
    if (url.protocol === 'http:' || url.protocol === 'https:') return `${url.protocol}//${url.host}/***`
  } catch {
    // Invalid URLs receive the same hidden label as missing URLs.
  }
  return t('admin.proxies.sourceUrlHidden')
}

function trafficText(source: ResourceItem): string {
  const used = Math.max(0, Number(source.sub_traffic_used) || 0)
  const total = Math.max(0, Number(source.sub_traffic_total) || 0)
  if (used <= 0 && total <= 0) return '-'
  if (total <= 0) return mr('sources.trafficUsedOnly', { used: formatBytes(used) })
  return mr('sources.trafficRemaining', { remaining: formatBytes(Math.max(0, total - used)), total: formatBytes(total) })
}

function syncStatus(value: unknown): { class: string; label: string } {
  switch (String(value || '').toLowerCase()) {
    case 'success': return { class: 'badge-success', label: 'admin.proxies.sourceStatusSuccess' }
    case 'partial': return { class: 'badge-warning', label: 'admin.proxies.sourceStatusPartial' }
    case 'error': return { class: 'badge-danger', label: 'admin.proxies.sourceStatusError' }
    case 'syncing': return { class: 'badge-primary', label: 'admin.proxies.sourceStatusSyncing' }
    default: return { class: 'badge-gray', label: 'admin.proxies.sourceStatusPending' }
  }
}

watch(() => props.show, show => {
  ++loadRequest
  loading.value = false
  editorOpen.value = false
  pendingDelete.value = null
  resetForm()
  if (show) {
    errorMessage.value = ''
    syncAllResult.value = null
    sources.value = []
    Object.assign(pagination, { page: 1, total: 0, pages: 1 })
    void loadSources(1)
  }
}, { immediate: true })

onUnmounted(() => { ++loadRequest })
</script>

<style scoped>
.proxy-source-table :deep(.table-wrapper) {
  max-height: 55vh;
}
</style>
