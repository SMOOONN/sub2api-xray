<template>
  <AppLayout>
    <div class="space-y-6 pb-12">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div><h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.retryFilter.monitorTitle') }}</h1><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.retryFilter.monitorDescription') }}</p></div>
        <div class="flex flex-wrap gap-2"><router-link class="btn btn-secondary" to="/admin/retry-filter">{{ t('admin.retryFilter.manageLink') }}</router-link><button class="btn btn-secondary" :disabled="loading" @click="load">{{ t('common.refresh') }}</button></div>
      </header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>

      <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <div v-for="card in summaryCards" :key="card.label" class="card p-4"><p class="text-xs font-medium uppercase tracking-wide text-gray-500">{{ card.label }}</p><p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ card.value }}</p></div>
      </div>

      <section class="card p-5">
        <div class="flex flex-wrap items-center justify-between gap-3"><h2 class="text-lg font-semibold">{{ t('admin.retryFilter.filters') }}</h2><div class="flex items-center gap-3 text-sm text-gray-700 dark:text-gray-300"><span>{{ t('admin.retryFilter.autoRefresh') }}</span><Toggle v-model="autoRefresh" :aria-label="t('admin.retryFilter.autoRefresh')" /></div></div>
        <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div><label for="retry-filter-monitor-range" class="input-label">{{ t('admin.retryFilter.timeRange') }}</label><Select id="retry-filter-monitor-range" v-model="filters.range" :options="rangeOptions" :aria-label="t('admin.retryFilter.timeRange')" :searchable="false" /></div>
          <div><label for="retry-filter-monitor-action" class="input-label">{{ t('admin.retryFilter.action') }}</label><Select id="retry-filter-monitor-action" v-model="filters.action" :options="actionOptions" :aria-label="t('admin.retryFilter.action')" :searchable="false" /></div>
          <label><span class="input-label">{{ t('admin.retryFilter.statusCodes') }}</span><input v-model="filters.status_code" class="input" inputmode="numeric" placeholder="429" /></label>
          <label><span class="input-label">{{ t('admin.retryFilter.models') }}</span><input v-model="filters.model" class="input" :placeholder="t('admin.retryFilter.modelPlaceholder')" /></label>
          <label><span class="input-label">{{ t('admin.retryFilter.accountID') }}</span><input v-model="filters.account_id" class="input" inputmode="numeric" /></label>
          <label><span class="input-label">{{ t('admin.retryFilter.groupID') }}</span><input v-model="filters.group_id" class="input" inputmode="numeric" /></label>
          <label><span class="input-label">{{ t('admin.retryFilter.globalDefault') }} ID</span><input v-model="filters.preset_id" class="input" inputmode="numeric" /></label>
          <label><span class="input-label">{{ t('admin.retryFilter.ruleID') }}</span><input v-model="filters.rule_id" class="input" inputmode="numeric" /></label>
          <label><span class="input-label">{{ t('admin.retryFilter.reason') }}</span><input v-model="filters.reason" class="input" /></label>
          <label class="sm:col-span-2"><span class="input-label">{{ t('admin.retryFilter.search') }}</span><input v-model="filters.q" class="input" :placeholder="t('admin.retryFilter.monitorSearchPlaceholder')" @keyup.enter="applyFilters" /></label>
        </div>
        <div class="mt-4 flex justify-end gap-2"><button class="btn btn-secondary" @click="resetFilters">{{ t('common.reset') }}</button><button class="btn btn-primary" :disabled="loading" @click="applyFilters">{{ t('common.search') }}</button></div>
      </section>

      <section class="card overflow-hidden">
        <div v-if="loading" class="py-12 text-center text-gray-500">{{ t('common.loading') }}</div>
        <div v-else-if="!events.length" class="py-14 text-center"><p class="font-medium">{{ t('admin.retryFilter.noMonitorEvents') }}</p><p class="mt-2 text-sm text-gray-500">{{ t('admin.retryFilter.noMonitorEventsHint') }}</p></div>
        <div v-else class="relative overflow-x-auto"><table class="w-full text-left text-sm"><thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-700"><tr><th class="px-5 py-3">{{ t('admin.retryFilter.time') }}</th><th class="px-4 py-3">{{ t('admin.retryFilter.rule') }}</th><th class="px-4 py-3">{{ t('admin.retryFilter.action') }}</th><th class="px-4 py-3">{{ t('admin.retryFilter.reason') }}</th><th class="px-4 py-3">{{ t('admin.retryFilter.requestContext') }}</th><th class="px-4 py-3"><span class="sr-only">{{ t('common.actions') }}</span></th></tr></thead><tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="event in events" :key="String(event.id)" class="hover:bg-gray-50 dark:hover:bg-dark-800"><td class="whitespace-nowrap px-5 py-4 text-xs text-gray-500">{{ formatDate(event.created_at) }}</td><td class="px-4 py-4"><p class="font-medium">{{ event.rule_name || `#${event.rule_id || '—'}` }}</p><p class="text-xs text-gray-500">{{ event.preset_name || (event.preset_id ? `#${event.preset_id}` : '—') }}</p></td><td class="px-4 py-4"><span class="rounded-full px-2.5 py-1 text-xs" :class="actionClass(event.action)">{{ actionLabel(event.action) }}</span></td><td class="max-w-xs px-4 py-4"><span class="break-words text-xs text-gray-600 dark:text-gray-300">{{ event.reason || '—' }}</span></td><td class="px-4 py-4 text-xs text-gray-500"><span>{{ event.model || '—' }}</span><span v-if="event.status_code" class="ml-2">HTTP {{ event.status_code }}</span><span v-if="event.account_name || event.account_id" class="ml-2">{{ event.account_name || `#${event.account_id}` }}</span></td><td class="whitespace-nowrap px-4 py-4"><button class="text-primary-600 dark:text-primary-400" @click="openDetail(event)">{{ t('common.view') }}</button></td></tr></tbody></table></div>
        <div v-if="!loading && events.length" class="flex items-center justify-between border-t border-gray-100 px-5 py-3 text-xs text-gray-500 dark:border-dark-700"><span>{{ t('admin.retryFilter.totalEvents', { count: total }) }}</span><span class="flex items-center gap-2"><button class="rounded px-2 py-1 hover:bg-gray-100 disabled:opacity-40 dark:hover:bg-dark-700" :disabled="page <= 1" @click="previousPage">←</button><span>{{ page }} / {{ pages }}</span><button class="rounded px-2 py-1 hover:bg-gray-100 disabled:opacity-40 dark:hover:bg-dark-700" :disabled="page >= pages" @click="nextPage">→</button></span></div>
      </section>
    </div>

    <BaseDialog :show="!!detail" :title="t('admin.retryFilter.monitorDetail')" width="wide" @close="detail = null"><dl v-if="detail" class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2"><dt class="text-gray-500">{{ t('admin.retryFilter.time') }}</dt><dd>{{ formatDate(detail.created_at) }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.requestID') }}</dt><dd class="break-all font-mono">{{ detail.request_id || '—' }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.rule') }}</dt><dd>{{ detail.rule_name || detail.rule_id || '—' }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.action') }}</dt><dd>{{ actionLabel(detail.action) }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.reason') }}</dt><dd class="break-words">{{ detail.reason || '—' }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.statusCodes') }}</dt><dd>{{ detail.status_code || '—' }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.models') }}</dt><dd>{{ detail.model || '—' }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.accountID') }}</dt><dd>{{ detail.account_name || detail.account_id || '—' }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.groupID') }}</dt><dd>{{ detail.group_name || detail.group_id || '—' }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.retryCount') }}</dt><dd>{{ detail.retry_count ?? '—' }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.retryDelay') }}</dt><dd>{{ detail.retry_delay_ms == null ? '—' : `${detail.retry_delay_ms} ms` }}</dd><dt class="text-gray-500">{{ t('admin.retryFilter.details') }}</dt><dd class="break-words">{{ detail.detail || '—' }}</dd></dl></BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import retryFilterAPI, { type RetryFilterAction, type RetryFilterMonitorEvent, type RetryFilterMonitorSummary } from '@/api/admin/retryFilter'
import { useAppStore } from '@/stores/app'

const { t } = useI18n(); const app = useAppStore()
const actions: RetryFilterAction[] = ['same_account_retry', 'switch_account', 'terminate', 'observe']
const rangeOptions = computed(() => ['15m', '1h', '6h', '24h'].map(value => ({ value, label: t(`admin.retryFilter.ranges.${value}`) })))
const actionOptions = computed(() => [{ value: '', label: t('admin.retryFilter.allActions') }, ...actions.map(value => ({ value, label: t(`admin.retryFilter.actions.${value}`) }))])
const loading = ref(false), error = ref(''), events = ref<RetryFilterMonitorEvent[]>([]), detail = ref<RetryFilterMonitorEvent | null>(null), total = ref(0), page = ref(1), pages = ref(1), autoRefresh = ref(true), summary = ref<RetryFilterMonitorSummary>({ matched: 0, retries: 0, terminates: 0, observations: 0, switches: 0 })
const filters = reactive({ range: '1h', action: '', status_code: '', model: '', account_id: '', group_id: '', preset_id: '', rule_id: '', reason: '', q: '' })
let timer: ReturnType<typeof setInterval> | undefined
const summaryCards = computed(() => {
  const byAction = summary.value.by_action || {}
  const recent = Array.isArray(summary.value.recent_errors) ? summary.value.recent_errors.length : (summary.value.recent_errors || 0)
  return [
    { label: t('admin.retryFilter.matchedCount'), value: summary.value.matched || total.value },
    { label: t('admin.retryFilter.retryActions'), value: summary.value.retries || byAction.same_account_retry || 0 },
    { label: t('admin.retryFilter.switchActions'), value: summary.value.switches || byAction.switch_account || 0 },
    { label: t('admin.retryFilter.terminateActions'), value: summary.value.terminates || byAction.terminate || 0 },
    { label: t('admin.retryFilter.recentErrors'), value: recent }
  ]
})
function rangeStart() { const ranges: Record<string, number> = { '15m': 15 * 60e3, '1h': 60 * 60e3, '6h': 6 * 60 * 60e3, '24h': 24 * 60 * 60e3 }; const ms = ranges[filters.range] || 60 * 60e3; return new Date(Date.now() - ms).toISOString() }
function query() { return { page: page.value, page_size: 30, start_time: rangeStart(), action: filters.action || undefined, status_code: filters.status_code || undefined, model: filters.model || undefined, account_id: filters.account_id || undefined, group_id: filters.group_id || undefined, preset_id: filters.preset_id || undefined, rule_id: filters.rule_id || undefined, reason: filters.reason || undefined, q: filters.q || undefined } }
async function load() { loading.value = true; error.value = ''; try { const result = await retryFilterAPI.listMonitor(query()); events.value = result.items || []; total.value = result.total || events.value.length; pages.value = Math.max(1, result.pages || Math.ceil(total.value / (result.page_size || 30))); summary.value = result.summary || { matched: total.value, retries: events.value.filter(e => e.action === 'same_account_retry').length, terminates: events.value.filter(e => e.action === 'terminate').length, switches: events.value.filter(e => e.action === 'switch_account').length, observations: events.value.filter(e => e.action === 'observe').length } } catch (e) { error.value = (e as { response?: { data?: { message?: string } } })?.response?.data?.message || t('admin.retryFilter.monitorLoadFailed') } finally { loading.value = false } }
function applyFilters() { page.value = 1; void load() }
function previousPage() { if (page.value > 1) { page.value -= 1; void load() } }
function nextPage() { if (page.value < pages.value) { page.value += 1; void load() } }
function resetFilters() { Object.assign(filters, { range: '1h', action: '', status_code: '', model: '', account_id: '', group_id: '', preset_id: '', rule_id: '', reason: '', q: '' }); applyFilters() }
function formatDate(value: string) { try { return new Date(value).toLocaleString() } catch { return value } }
function actionLabel(action?: string | null) { return action && actions.includes(action as RetryFilterAction) ? t(`admin.retryFilter.actions.${action}`) : action || '—' }
function actionClass(action?: string | null) { return action === 'terminate' ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : action === 'switch_account' ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300' : action === 'observe' ? 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200' : 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' }
async function openDetail(event: RetryFilterMonitorEvent) { try { detail.value = await retryFilterAPI.getMonitorEvent(event.id) } catch { detail.value = event; app.showError(t('admin.retryFilter.monitorDetailFailed')) } }
function refreshTimer() { if (timer) clearInterval(timer); timer = autoRefresh.value ? setInterval(() => { if (!loading.value) void load() }, 15000) : undefined }
watch(autoRefresh, refreshTimer); onMounted(() => { void load(); refreshTimer() }); onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>
