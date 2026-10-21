<template>
  <AppLayout>
    <div class="space-y-6 pb-12">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div><h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.retryFilter.monitorTitle') }}</h1><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.retryFilter.monitorDescription') }}</p></div>
        <div class="flex flex-wrap gap-2"><router-link class="btn btn-secondary" to="/admin/retry-filter">{{ t('admin.retryFilter.manageLink') }}</router-link><button class="btn btn-secondary" :disabled="loading" @click="load">{{ t('common.refresh') }}</button></div>
      </header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>

      <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <div v-for="card in summaryCards" :key="card.label" class="card p-4"><p class="text-xs font-medium uppercase tracking-wide text-gray-500">{{ card.label }}</p><p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ card.value }}</p></div>
      </div>

      <section class="card p-4 sm:p-5">
        <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-lg font-semibold">{{ t('admin.retryFilter.filters') }}</h2><div class="flex items-center gap-3 text-sm text-gray-700 dark:text-gray-300"><span>{{ t('admin.retryFilter.autoRefresh') }}</span><Toggle v-model="autoRefresh" :aria-label="t('admin.retryFilter.autoRefresh')" /></div></div>
        <div class="mt-4 grid min-w-0 grid-cols-2 items-end gap-2 sm:flex sm:flex-wrap">
          <div class="min-w-0 sm:w-36"><label for="retry-filter-monitor-range" class="input-label">{{ t('admin.retryFilter.timeRange') }}</label><Select id="retry-filter-monitor-range" v-model="filters.range" :options="rangeOptions" :aria-label="t('admin.retryFilter.timeRange')" :searchable="false" /></div>
          <div class="min-w-0 sm:w-36"><label for="retry-filter-monitor-action" class="input-label">{{ t('admin.retryFilter.action') }}</label><Select id="retry-filter-monitor-action" v-model="filters.action" :options="actionOptions" :aria-label="t('admin.retryFilter.action')" :searchable="false" /></div>
          <label class="col-span-2 min-w-0 sm:min-w-[220px] sm:flex-1"><span class="input-label">{{ t('admin.retryFilter.search') }}</span><input v-model="filters.q" class="input" :placeholder="t('admin.retryFilter.monitorSearchPlaceholder')" @keyup.enter="applyFilters" /></label>
          <button type="button" class="btn btn-secondary col-span-2 min-w-0 sm:w-auto" :aria-expanded="moreFiltersOpen" :aria-controls="moreFiltersId" :aria-label="activeSecondaryCount ? t('admin.retryFilter.moreFiltersActive', { count: activeSecondaryCount }) : t('admin.retryFilter.moreFilters')" @click="moreFiltersOpen = !moreFiltersOpen">{{ t('admin.retryFilter.moreFilters') }}<span v-if="activeSecondaryCount" aria-hidden="true" class="ml-2 rounded-full bg-primary-100 px-2 text-xs font-medium text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">{{ activeSecondaryCount }}</span></button>
        </div>
        <div v-show="moreFiltersOpen" :id="moreFiltersId" class="mt-3 grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-3 lg:grid-cols-4">
          <label class="min-w-0"><span class="input-label">{{ t('admin.retryFilter.statusCodes') }}</span><input v-model="filters.status_code" class="input" inputmode="numeric" placeholder="429" /></label>
          <label class="min-w-0"><span class="input-label">{{ t('admin.retryFilter.models') }}</span><input v-model="filters.model" class="input" :placeholder="t('admin.retryFilter.modelPlaceholder')" /></label>
          <label class="min-w-0"><span class="input-label">{{ t('admin.retryFilter.accountID') }}</span><input v-model="filters.account_id" class="input" inputmode="numeric" /></label>
          <label class="min-w-0"><span class="input-label">{{ t('admin.retryFilter.groupID') }}</span><input v-model="filters.group_id" class="input" inputmode="numeric" /></label>
          <label class="min-w-0"><span class="input-label">{{ t('admin.retryFilter.globalDefault') }} ID</span><input v-model="filters.preset_id" class="input" inputmode="numeric" /></label>
          <label class="min-w-0"><span class="input-label">{{ t('admin.retryFilter.ruleID') }}</span><input v-model="filters.rule_id" class="input" inputmode="numeric" /></label>
          <label class="min-w-0 sm:col-span-3 lg:col-span-2"><span class="input-label">{{ t('admin.retryFilter.reason') }}</span><input v-model="filters.reason" class="input" /></label>
        </div>
        <div class="mt-4 flex justify-end gap-2 border-t border-gray-100 pt-4 dark:border-dark-700"><button class="btn btn-secondary" @click="resetFilters">{{ t('common.reset') }}</button><button class="btn btn-primary" :disabled="loading" @click="applyFilters">{{ t('common.search') }}</button></div>
      </section>

      <section class="card overflow-hidden" :aria-busy="loading">
        <span v-if="loading" class="sr-only" role="status">{{ t('common.loading') }}</span>
        <DataTable :columns="columns" :data="events" :loading="loading" row-key="id">
          <template #cell-created_at="{ value }"><time :datetime="value" class="whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">{{ formatDate(value) }}</time></template>
          <template #cell-rule="{ row }"><div class="min-w-0 max-w-[220px]"><div class="truncate font-medium text-gray-900 dark:text-white" :title="row.rule_name || undefined">{{ row.rule_name || `#${row.rule_id || '—'}` }}</div><div class="mt-0.5 truncate text-xs text-gray-500">{{ row.preset_name || (row.preset_id ? `#${row.preset_id}` : '—') }}</div></div></template>
          <template #cell-action="{ row }"><span class="inline-flex rounded-full px-2.5 py-1 text-xs" :class="actionClass(row.action)">{{ actionLabel(row.action) }}</span></template>
          <template #cell-reason="{ row }"><span class="block max-w-[280px] whitespace-normal break-words text-xs text-gray-600 dark:text-gray-300">{{ row.reason || '—' }}</span></template>
          <template #cell-request_context="{ row }"><div class="max-w-[220px] whitespace-normal space-y-0.5 text-xs text-gray-500 dark:text-gray-400"><div v-if="row.model" class="break-all font-medium text-gray-700 dark:text-gray-200">{{ row.model }}</div><div v-if="row.status_code || row.account_name || row.account_id" class="break-words"><span v-if="row.status_code">HTTP {{ row.status_code }}</span><span v-if="row.status_code && (row.account_name || row.account_id)"> · </span><span v-if="row.account_name || row.account_id">{{ row.account_name || `#${row.account_id}` }}</span></div><div v-if="row.group_name || row.group_id" class="break-words">{{ row.group_name || `#${row.group_id}` }}</div><span v-if="!row.model && !row.status_code && !row.account_name && !row.account_id && !row.group_name && !row.group_id">—</span></div></template>
          <template #cell-actions="{ row }"><button type="button" class="inline-flex items-center font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300" @click="openDetail(row)">{{ t('common.view') }}</button></template>
          <template #empty><div class="flex flex-col items-center py-10"><p class="text-sm font-medium text-gray-500 dark:text-gray-400">{{ t('admin.retryFilter.noMonitorEvents') }}</p><p class="mt-2 text-xs text-gray-400 dark:text-gray-500">{{ t('admin.retryFilter.noMonitorEventsHint') }}</p></div></template>
        </DataTable>
        <Pagination v-if="total > 0" :total="total" :page="page" :page-size="pageSize" :show-page-size-selector="false" @update:page="onPageChange" />
      </section>
    </div>

    <BaseDialog :show="!!detail" :title="t('admin.retryFilter.monitorDetail')" width="wide" @close="detail = null">
      <div v-if="detail" class="space-y-5 py-2">
        <div class="rounded-2xl border border-gray-200 bg-gray-50/60 p-5 dark:border-dark-700 dark:bg-dark-900/60">
          <div class="flex flex-wrap items-center gap-3"><span class="rounded-full px-2.5 py-1 text-xs" :class="actionClass(detail.action)">{{ actionLabel(detail.action) }}</span><span class="break-all font-mono text-base font-semibold text-gray-900 dark:text-white">{{ detail.rule_name || `#${detail.rule_id || '—'}` }}</span></div>
          <div class="mt-3 flex flex-wrap items-center gap-x-5 gap-y-1.5 text-xs text-gray-500 dark:text-gray-400"><time :datetime="detail.created_at">{{ formatDate(detail.created_at) }}</time><span v-if="detail.request_id" class="break-all font-mono">{{ t('admin.retryFilter.requestID') }}: {{ detail.request_id }}</span><span v-if="detail.preset_name || detail.preset_id" class="min-w-0 break-all">{{ detail.preset_name || `#${detail.preset_id}` }}</span></div>
        </div>
        <dl class="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><dt class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.retryFilter.reason') }}</dt><dd class="mt-1 break-words text-gray-900 dark:text-white">{{ detail.reason || '—' }}</dd></div>
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><dt class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.retryFilter.models') }}</dt><dd class="mt-1 break-words text-gray-900 dark:text-white">{{ detail.model || '—' }}</dd></div>
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><dt class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.retryFilter.statusCodes') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ detail.status_code || '—' }}</dd></div>
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><dt class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.retryFilter.accountID') }}</dt><dd class="mt-1 break-words text-gray-900 dark:text-white">{{ detail.account_name || detail.account_id || '—' }}</dd></div>
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><dt class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.retryFilter.groupID') }}</dt><dd class="mt-1 break-words text-gray-900 dark:text-white">{{ detail.group_name || detail.group_id || '—' }}</dd></div>
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><dt class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.retryFilter.retryCount') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ detail.retry_count ?? '—' }}</dd></div>
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><dt class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.retryFilter.retryDelay') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ detail.retry_delay_ms == null ? '—' : `${detail.retry_delay_ms} ms` }}</dd></div>
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900 sm:col-span-2"><dt class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.retryFilter.details') }}</dt><dd class="mt-1 break-words text-gray-900 dark:text-white">{{ detail.detail || '—' }}</dd></div>
        </dl>
      </div>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import type { Column } from '@/components/common/types'
import Toggle from '@/components/common/Toggle.vue'
import retryFilterAPI, { type RetryFilterAction, type RetryFilterMonitorEvent, type RetryFilterMonitorSummary } from '@/api/admin/retryFilter'
import { useAppStore } from '@/stores/app'

const { t } = useI18n(); const app = useAppStore()
const actions: RetryFilterAction[] = ['same_account_retry', 'switch_account', 'terminate', 'observe']
const rangeOptions = computed(() => ['15m', '1h', '6h', '24h'].map(value => ({ value, label: t(`admin.retryFilter.ranges.${value}`) })))
const actionOptions = computed(() => [{ value: '', label: t('admin.retryFilter.allActions') }, ...actions.map(value => ({ value, label: t(`admin.retryFilter.actions.${value}`) }))])
const columns = computed<Column[]>(() => [
  { key: 'created_at', label: t('admin.retryFilter.time') },
  { key: 'rule', label: t('admin.retryFilter.rule') },
  { key: 'action', label: t('admin.retryFilter.action') },
  { key: 'reason', label: t('admin.retryFilter.reason') },
  { key: 'request_context', label: t('admin.retryFilter.requestContext') },
  { key: 'actions', label: t('common.actions') }
])
const loading = ref(false), error = ref(''), events = ref<RetryFilterMonitorEvent[]>([]), detail = ref<RetryFilterMonitorEvent | null>(null), total = ref(0), page = ref(1), pageSize = ref(30), autoRefresh = ref(true), moreFiltersOpen = ref(false), moreFiltersId = useId(), summary = ref<RetryFilterMonitorSummary>({ matched: 0, retries: 0, terminates: 0, observations: 0, switches: 0 })
const filters = reactive({ range: '1h', action: '', status_code: '', model: '', account_id: '', group_id: '', preset_id: '', rule_id: '', reason: '', q: '' })
let timer: ReturnType<typeof setInterval> | undefined
let loadRequestId = 0
const activeSecondaryCount = computed(() => ['status_code', 'model', 'account_id', 'group_id', 'preset_id', 'rule_id', 'reason'].filter(key => { const value = filters[key as keyof typeof filters]; return value !== '' && value !== null && value !== undefined }).length)
const summaryCards = computed(() => {
  const byAction = summary.value.by_action || {}
  return [
    { label: t('admin.retryFilter.matchedCount'), value: summary.value.matched || total.value },
    { label: t('admin.retryFilter.retryActions'), value: summary.value.retries || byAction.same_account_retry || 0 },
    { label: t('admin.retryFilter.switchActions'), value: summary.value.switches || byAction.switch_account || 0 },
    { label: t('admin.retryFilter.terminateActions'), value: summary.value.terminates || byAction.terminate || 0 }
  ]
})
function rangeStart() { const ranges: Record<string, number> = { '15m': 15 * 60e3, '1h': 60 * 60e3, '6h': 6 * 60 * 60e3, '24h': 24 * 60 * 60e3 }; const ms = ranges[filters.range] || 60 * 60e3; return new Date(Date.now() - ms).toISOString() }
function query() { return { page: page.value, page_size: pageSize.value, start_time: rangeStart(), action: filters.action || undefined, status_code: filters.status_code || undefined, model: filters.model || undefined, account_id: filters.account_id || undefined, group_id: filters.group_id || undefined, preset_id: filters.preset_id || undefined, rule_id: filters.rule_id || undefined, reason: filters.reason || undefined, q: filters.q || undefined } }
async function load() {
  const requestId = ++loadRequestId
  loading.value = true
  error.value = ''
  try {
    const result = await retryFilterAPI.listMonitor(query())
    if (requestId !== loadRequestId) return
    events.value = result.items || []
    total.value = result.total || events.value.length
    page.value = result.page || page.value
    pageSize.value = result.page_size || pageSize.value
    summary.value = result.summary || {
      matched: total.value,
      retries: events.value.filter(e => e.action === 'same_account_retry').length,
      terminates: events.value.filter(e => e.action === 'terminate').length,
      switches: events.value.filter(e => e.action === 'switch_account').length,
      observations: events.value.filter(e => e.action === 'observe').length
    }
  } catch (e) {
    if (requestId !== loadRequestId) return
    error.value = (e as { response?: { data?: { message?: string } } })?.response?.data?.message || t('admin.retryFilter.monitorLoadFailed')
  } finally {
    if (requestId === loadRequestId) loading.value = false
  }
}
function applyFilters() { page.value = 1; void load() }
function onPageChange(value: number) { if (loading.value) return; page.value = value; void load() }
function resetFilters() { Object.assign(filters, { range: '1h', action: '', status_code: '', model: '', account_id: '', group_id: '', preset_id: '', rule_id: '', reason: '', q: '' }); applyFilters() }
function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return `${date.toLocaleString(undefined, { hour12: false })}.${String(date.getMilliseconds()).padStart(3, '0')}`
}
function actionLabel(action?: string | null) { return action && actions.includes(action as RetryFilterAction) ? t(`admin.retryFilter.actions.${action}`) : action || '—' }
function actionClass(action?: string | null) { return action === 'terminate' ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : action === 'switch_account' ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300' : action === 'observe' ? 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200' : 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' }
async function openDetail(event: RetryFilterMonitorEvent) { try { detail.value = await retryFilterAPI.getMonitorEvent(event.id) } catch { detail.value = event; app.showError(t('admin.retryFilter.monitorDetailFailed')) } }
function refreshTimer() { if (timer) clearInterval(timer); timer = autoRefresh.value ? setInterval(() => { if (!loading.value) void load() }, 15000) : undefined }
watch(autoRefresh, refreshTimer); onMounted(() => { void load(); refreshTimer() }); onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>
