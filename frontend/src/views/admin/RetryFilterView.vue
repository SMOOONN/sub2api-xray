<template>
  <AppLayout>
    <div class="space-y-6 pb-12">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.retryFilter.manageTitle') }}</h1>
            <span class="rounded-full bg-primary-50 px-2 py-0.5 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{ t('admin.retryFilter.ruleSets') }}</span>
          </div>
          <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.retryFilter.manageDescription') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" :disabled="loading || busy" @click="load">{{ t('common.refresh') }}</button>
          <router-link class="btn btn-secondary" to="/admin/retry-filter/monitor">{{ t('admin.retryFilter.monitorLink') }}</router-link>
          <router-link class="btn btn-secondary" to="/admin/settings">{{ t('admin.retryFilter.settings') }}</router-link>
        </div>
      </header>

      <div class="rounded-2xl border border-amber-200 bg-amber-50/80 p-4 text-sm text-amber-900 shadow-sm dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        <p class="font-semibold">{{ t('admin.retryFilter.limitsTitle') }}</p>
        <div class="mt-3 grid gap-3 lg:grid-cols-3">
          <p class="rounded-xl border border-amber-200/80 bg-white/50 p-3 leading-relaxed dark:border-amber-800/70 dark:bg-amber-950/20">{{ t('admin.retryFilter.scopeLimit') }}</p>
          <p class="rounded-xl border border-amber-200/80 bg-white/50 p-3 leading-relaxed dark:border-amber-800/70 dark:bg-amber-950/20">{{ t('admin.retryFilter.wsLimit') }}</p>
          <p class="rounded-xl border border-amber-200/80 bg-white/50 p-3 leading-relaxed dark:border-amber-800/70 dark:bg-amber-950/20">{{ t('admin.retryFilter.breakerLimit') }}</p>
        </div>
      </div>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <div v-if="loading" role="status" class="py-12 text-center text-gray-500">{{ t('common.loading') }}</div>

      <template v-else>
        <div class="grid items-start gap-5 xl:grid-cols-[240px_minmax(0,1fr)]">
          <aside class="card min-w-0 overflow-hidden xl:sticky xl:top-4">
            <div class="border-b border-gray-100 p-4 dark:border-dark-700">
              <div class="flex items-center justify-between gap-3">
                <div><h2 class="font-semibold">{{ t('admin.retryFilter.ruleSets') }}</h2><p class="mt-1 text-xs text-gray-500">{{ t('admin.retryFilter.ruleSetsHint') }}</p></div>
                <button class="btn btn-primary btn-sm" :disabled="busy" :title="t('admin.retryFilter.addRuleSet')" @click="createPreset">{{ t('common.add') }}</button>
              </div>
              <input v-model="presetSearch" class="input mt-4" :aria-label="t('admin.retryFilter.searchRuleSets')" :placeholder="t('admin.retryFilter.searchRuleSets')" />
            </div>
            <div class="max-h-72 space-y-1 overflow-y-auto p-2 xl:max-h-[calc(100vh-280px)]">
              <p v-if="!filteredPresets.length" class="p-4 text-sm text-gray-500">{{ t('admin.retryFilter.noPresets') }}</p>
              <button v-for="preset in filteredPresets" :key="preset.id" :aria-pressed="selectedID === preset.id" class="w-full rounded-lg border p-3 text-left transition-colors" :class="selectedID === preset.id ? 'border-primary-400 bg-primary-50 dark:bg-primary-900/20' : 'border-transparent hover:bg-gray-50 dark:hover:bg-dark-700'" @click="selectPreset(preset.id)">
                <span class="flex items-center justify-between gap-2"><span class="min-w-0 truncate font-medium" :title="preset.name">{{ preset.name }}</span><span class="h-2 w-2 shrink-0 rounded-full" :class="preset.enabled ? 'bg-emerald-500' : 'bg-gray-400'"></span></span>
                <span class="mt-1 block text-xs text-gray-500">#{{ preset.id }} · {{ ruleCount(preset.id) }} {{ t('admin.retryFilter.rules') }}<span v-if="defaultID === preset.id"> · {{ t('admin.retryFilter.globalDefault') }}</span></span>
              </button>
            </div>
          </aside>

          <section class="min-w-0 space-y-5">
            <section v-if="selectedPreset" class="card min-w-0 overflow-hidden" aria-labelledby="retry-filter-preset-title">
              <div class="p-3 sm:p-5">
                <div class="mb-4 flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 pb-4 dark:border-dark-700">
                  <h2 id="retry-filter-preset-title" class="text-lg font-semibold">{{ t('admin.retryFilter.editPreset') }}</h2>
                  <button class="btn btn-primary btn-sm" :disabled="busy || !presetDirty" @click="savePreset">{{ t('admin.retryFilter.savePreset') }}</button>
                </div>
                <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_180px]">
                  <div class="min-w-0">
                    <label><span class="input-label">{{ t('admin.retryFilter.name') }}</span><input v-model="presetForm.name" class="input text-lg font-semibold" maxlength="200" @change="presetDirty = true" /></label>
                    <label class="mt-3 block"><span class="input-label">{{ t('admin.retryFilter.notes') }}</span><textarea v-model="presetForm.description" rows="3" class="input resize-y" @change="presetDirty = true"></textarea></label>
                  </div>
                  <div class="min-w-0 rounded-xl bg-gray-50 p-4 dark:bg-dark-900/40">
                    <div class="flex items-center justify-between gap-3"><span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.retryFilter.enabled') }}</span><Toggle v-model="presetForm.enabled" :aria-label="t('admin.retryFilter.enabled')" @update:model-value="presetDirty = true" /></div>
                    <div class="mt-4 space-y-2 text-xs text-gray-500 dark:text-dark-400"><div class="flex justify-between gap-3"><span>ID</span><span class="font-mono text-gray-700 dark:text-gray-200">#{{ selectedPreset.id }}</span></div><div class="flex justify-between gap-3"><span>{{ t('admin.retryFilter.rules') }}</span><span class="font-medium text-gray-700 dark:text-gray-200">{{ rules.length }}</span></div><div v-if="defaultID === selectedPreset.id" class="text-primary-600 dark:text-primary-400">{{ t('admin.retryFilter.globalDefault') }}</div></div>
                  </div>
                </div>
                <div class="mt-4 flex flex-wrap justify-end gap-2 border-t border-gray-100 pt-4 dark:border-dark-700">
                  <button class="btn btn-secondary btn-sm" :disabled="busy" @click="duplicatePreset">{{ t('admin.retryFilter.duplicate') }}</button>
                  <button class="btn btn-secondary btn-sm text-red-600" :disabled="busy" @click="deleteTarget = { kind: 'preset', id: selectedPreset.id, name: selectedPreset.name }">{{ t('common.delete') }}</button>
                </div>
              </div>
            </section>

            <section v-else class="card p-8 text-center text-sm text-gray-500">{{ t('admin.retryFilter.selectPreset') }}</section>

            <section v-if="selectedPreset" class="card min-w-0 overflow-hidden" aria-labelledby="retry-filter-rules-title">
              <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 p-3 sm:p-5 dark:border-dark-700">
                <div class="min-w-0"><div class="flex items-center gap-2"><h2 id="retry-filter-rules-title" class="text-lg font-semibold">{{ t('admin.retryFilter.rulesTitle') }}</h2><span class="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-700 dark:text-dark-300">{{ rules.length }}</span></div><p class="mt-1 text-sm text-gray-500">{{ t('admin.retryFilter.dragHint') }}</p></div>
                <button class="btn btn-primary btn-sm" :disabled="busy || !!editingRule" @click="beginRule()">{{ t('admin.retryFilter.addRule') }}</button>
              </div>
              <div class="p-3 sm:p-5">
                <div v-if="rulesLoading" class="py-8 text-center text-gray-500">{{ t('common.loading') }}</div>
                <div v-else-if="!rules.length" class="rounded-xl border border-dashed border-gray-300 p-8 text-center dark:border-dark-600"><p class="font-medium">{{ t('admin.retryFilter.noRules') }}</p><p class="mt-2 text-sm text-gray-500">{{ t('admin.retryFilter.fallbackHint') }}</p></div>
                <div v-else class="space-y-3">
                  <div v-for="(rule, index) in rules" :key="rule.id" :data-rule-id="rule.id" class="min-w-0 rounded-xl border border-gray-200 bg-white transition-colors hover:border-primary-300 dark:border-dark-700 dark:bg-dark-800/40 dark:hover:border-primary-700" :class="!rule.enabled ? 'opacity-70' : ''" draggable="true" @dragstart="startDrag(index)" @dragover.prevent @drop="dropRule(index)">
                    <div class="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-3 p-3 sm:p-4 2xl:grid-cols-[auto_minmax(0,1fr)_auto]">
                      <div class="flex items-center gap-2"><button class="cursor-grab rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-700" :title="t('admin.retryFilter.dragToReorder')" @mousedown.stop><span aria-hidden="true">⋮⋮</span><span class="sr-only">{{ t('admin.retryFilter.dragToReorder') }}</span></button><span class="flex h-7 w-7 items-center justify-center rounded-full bg-gray-100 font-mono text-xs text-gray-500 dark:bg-dark-700">{{ index + 1 }}</span></div>
                      <button class="min-w-0 text-left" :disabled="busy" @click="beginRule(rule)">
                        <span class="block break-words font-medium text-gray-900 [overflow-wrap:anywhere] dark:text-white">{{ rule.name }}</span>
                        <span class="mt-1 block space-y-1 text-sm text-gray-500">
                          <span v-for="(condition, conditionIndex) in ruleSummary(rule)" :key="conditionIndex" class="block break-words [overflow-wrap:anywhere]">{{ condition }}</span>
                          <span v-if="!ruleSummary(rule).length">{{ t('admin.retryFilter.noConditions') }}</span>
                        </span>
                        <span class="mt-2 flex flex-wrap gap-2">
                          <span class="max-w-full rounded-full bg-primary-50 px-2.5 py-1 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ t(`admin.retryFilter.actions.${rule.action}`) }}</span>
                          <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-dark-300">{{ rule.enabled ? t('admin.retryFilter.enabled') : t('admin.retryFilter.disabled') }}</span>
                        </span>
                      </button>
                      <div class="col-span-2 flex flex-wrap items-center justify-end gap-2 border-t border-gray-100 pt-3 2xl:col-span-1 2xl:border-t-0 2xl:pt-0 dark:border-dark-700">
                        <div class="mr-auto 2xl:mr-1"><Toggle :aria-label="rule.enabled ? t('admin.retryFilter.disable') : t('admin.retryFilter.enable')" :model-value="rule.enabled" :disabled="busy" @update:model-value="toggleRule(rule)" /></div>
                        <button class="btn btn-secondary btn-sm" :title="t('common.edit')" :disabled="busy" @click="beginRule(rule)">{{ t('common.edit') }}</button>
                        <button class="btn btn-secondary btn-sm" :title="t('admin.retryFilter.duplicate')" :disabled="busy" @click="duplicateRule(rule)">{{ t('admin.retryFilter.duplicate') }}</button>
                        <button class="btn btn-secondary btn-sm text-red-600" :title="t('common.delete')" :disabled="busy" @click="deleteTarget = { kind: 'rule', id: rule.id, presetID: selectedID!, name: rule.name }">{{ t('common.delete') }}</button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </section>

            <section v-if="selectedPreset" class="card min-w-0 p-3 sm:p-5" aria-labelledby="retry-filter-scope-title">
              <div class="flex flex-wrap items-start justify-between gap-3">
                <div class="min-w-0"><h2 id="retry-filter-scope-title" class="text-lg font-semibold">{{ t('admin.retryFilter.applyScope') }}</h2><p class="mt-1 text-sm text-gray-500">{{ t('admin.retryFilter.applyScopeHint') }}</p></div>
                <button class="btn btn-primary btn-sm" :disabled="busy" @click="saveBindings">{{ t('admin.retryFilter.saveBindings') }}</button>
              </div>
              <label class="mt-4 flex items-start gap-3 rounded-xl bg-gray-50 p-3 text-sm dark:bg-dark-900/40">
                <input v-model="isGlobalDefault" class="mt-1 h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700" type="checkbox" aria-describedby="retry-filter-default-hint" />
                <span class="min-w-0"><span class="block font-medium">{{ t('admin.retryFilter.setAsGlobalDefault') }}</span><span id="retry-filter-default-hint" class="mt-1 block text-xs leading-relaxed text-gray-500">{{ t('admin.retryFilter.defaultHint') }}</span></span>
              </label>
              <div class="mt-4 flex flex-wrap items-center gap-2" role="group" :aria-label="t('admin.retryFilter.applyScope')">
                <button v-for="kind in bindingKinds" :key="kind" :data-binding-kind="kind" :aria-pressed="bindingKind === kind" class="rounded-lg px-3 py-2 text-sm" :class="bindingKind === kind ? 'bg-primary-600 text-white' : 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200'" @click="bindingKind = kind">
                  {{ t(`admin.retryFilter.${kind}`) }} <span class="ml-1 text-xs">({{ kind === 'accounts' ? selectedAccountIDs.size : selectedGroupIDs.size }})</span>
                </button>
              </div>
              <div class="mt-4 min-w-0 rounded-xl border border-gray-200 p-3 sm:p-4 dark:border-dark-700">
                <div class="flex flex-wrap items-center gap-3">
                  <input v-model="bindingSearch" class="input min-w-0 flex-1 basis-full sm:basis-0" :aria-label="t('admin.retryFilter.searchBinding', { kind: t(`admin.retryFilter.${bindingKind}`) })" :placeholder="t('admin.retryFilter.searchBinding', { kind: t(`admin.retryFilter.${bindingKind}`) })" @keyup.enter="loadCandidates" />
                  <button class="btn btn-secondary btn-sm" :disabled="candidateLoading" @click="loadCandidates">{{ t('common.search') }}</button>
                </div>
                <div class="mt-3 flex min-h-8 min-w-0 flex-wrap gap-2">
                  <span v-for="item in selectedCandidateItems" :key="item.id" class="inline-flex max-w-full items-start gap-2 rounded-xl bg-primary-50 px-2.5 py-1 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                    <span class="min-w-0 break-words [overflow-wrap:anywhere]">{{ item.name }}</span><button type="button" class="shrink-0 font-semibold" :aria-label="t('admin.retryFilter.removeBinding', { name: item.name })" @click="toggleCandidate(item)">×</button>
                  </span>
                  <span v-if="!selectedCandidateItems.length" class="text-xs text-gray-500">{{ t('admin.retryFilter.noneSelected') }}</span>
                </div>
                <div v-if="candidateLoading" class="py-6 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
                <div v-else class="mt-3 overflow-hidden rounded border border-gray-100 dark:border-dark-700">
                  <div class="hidden grid-cols-[1.25rem_3rem_minmax(0,1fr)_5rem_5.5rem] gap-2 border-b border-gray-100 bg-gray-50 px-3 py-2 text-xs font-medium text-gray-500 sm:grid dark:border-dark-700 dark:bg-dark-800/50 dark:text-dark-300">
                    <span></span><span>{{ t('admin.retryFilter.idLabel') }}</span><span>{{ t('admin.retryFilter.name') }}</span><span>{{ t('admin.retryFilter.platform') }}</span><span>{{ t('admin.retryFilter.status') }}</span>
                  </div>
                  <label v-for="item in candidateItems" :key="item.id" :data-candidate-id="item.id" class="grid cursor-pointer grid-cols-[1.25rem_minmax(0,1fr)] items-center gap-x-2 gap-y-1 border-b border-gray-100 px-3 py-3 text-sm last:border-b-0 hover:bg-gray-50 sm:grid-cols-[1.25rem_3rem_minmax(0,1fr)_5rem_5.5rem] sm:gap-2 sm:py-2 dark:border-dark-700 dark:hover:bg-dark-800">
                    <input class="row-span-4 h-4 w-4 self-start rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700 sm:row-span-1 sm:self-center" type="checkbox" :checked="selectedIDs.has(item.id)" @change="toggleCandidate(item)" />
                    <span class="min-w-0 break-all font-mono text-xs text-gray-500"><span class="sm:hidden">{{ t('admin.retryFilter.idLabel') }}: </span>{{ item.id }}</span>
                    <span class="col-start-2 min-w-0 break-words [overflow-wrap:anywhere] sm:col-auto" :title="item.name">{{ item.name }}</span>
                    <span class="col-start-2 min-w-0 break-words text-xs text-gray-500 [overflow-wrap:anywhere] sm:col-auto" :title="item.platform || ''"><span class="sm:hidden">{{ t('admin.retryFilter.platform') }}: </span>{{ item.platform || '—' }}</span>
                    <span v-if="item.eligible === false" class="col-start-2 text-xs text-amber-600 sm:col-auto" :title="t('admin.retryFilter.ineligible')">{{ t('admin.retryFilter.notEligible') }}</span><span v-else class="col-start-2 text-xs text-emerald-600 sm:col-auto">{{ t('admin.retryFilter.eligible') }}</span>
                  </label>
                  <p v-if="!candidateItems.length" class="px-3 py-6 text-center text-sm text-gray-500">{{ t('admin.retryFilter.noCandidates') }}</p>
                </div>
                <div class="mt-3 flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500"><span>{{ t('admin.retryFilter.selectedCount', { count: selectedIDs.size }) }}</span><span class="flex items-center gap-2"><button class="rounded px-2 py-1 hover:bg-gray-100 disabled:opacity-40 dark:hover:bg-dark-700" :aria-label="t('pagination.previous')" :disabled="candidatePage <= 1" @click="changeCandidatePage(-1)">←</button><span>{{ candidatePage }} / {{ candidatePages || 1 }}</span><button class="rounded px-2 py-1 hover:bg-gray-100 disabled:opacity-40 dark:hover:bg-dark-700" :aria-label="t('pagination.next')" :disabled="candidatePage >= candidatePages" @click="changeCandidatePage(1)">→</button></span></div>
              </div>
              <p v-if="bindingError" role="alert" class="mt-3 text-sm text-amber-700 dark:text-amber-300">{{ bindingError }}</p>
            </section>
          </section>
        </div>
      </template>
    </div>

    <BaseDialog :show="!!deleteTarget" :title="t('common.delete')" @close="!busy && (deleteTarget = null)"><p>{{ t('admin.retryFilter.deleteConfirm', { name: deleteTarget?.name }) }}</p><p class="mt-2 text-sm text-gray-500">{{ t('admin.retryFilter.deleteHint') }}</p><p v-if="dialogError" role="alert" class="mt-2 text-sm text-red-600">{{ dialogError }}</p><template #footer><button class="btn btn-secondary" :disabled="busy" @click="deleteTarget = null">{{ t('common.cancel') }}</button><button class="btn btn-danger" :disabled="busy" @click="confirmDelete">{{ t('common.delete') }}</button></template></BaseDialog>
    <BaseDialog
      :show="editingRule !== null"
      :title="editingRule === 'new' ? t('admin.retryFilter.addRule') : t('admin.retryFilter.editRule')"
      width="extra-wide"
      :close-on-escape="!busy"
      @close="!busy && cancelRule()"
    >
      <RuleEditor :draft="ruleForm" :error="dialogError" :status-error="ruleStatusError" @save="saveRule" />
      <template #footer>
        <button class="btn btn-secondary" :disabled="busy" @click="cancelRule">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" :disabled="busy" form="retry-filter-rule-editor" type="submit">{{ t('common.save') }}</button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import retryFilterAPI, { type RetryFilterAction, type RetryFilterBindingCandidate, type RetryFilterEmptyRequestMode, type RetryFilterEmptyResponseMode, type RetryFilterPreset, type RetryFilterRule, type RetryFilterRuleInput } from '@/api/admin/retryFilter'
import accountsAPI from '@/api/admin/accounts'
import groupsAPI from '@/api/admin/groups'
import { useAppStore } from '@/stores/app'

type BindingKind = 'accounts' | 'groups'
type RuleDraft = {
  name: string; description: string; enabled: boolean; priority: number; match_mode: 'any' | 'all'; action: RetryFilterAction
  statusText: string; keywordsText: string; codesText: string; pathsText: string; modelsText: string
  retryCount: string; retryDelay: string; terminateStatus: string; terminateMessage: string
  emptyRequestMode: RetryFilterEmptyRequestMode; emptyResponseMode: RetryFilterEmptyResponseMode
}

const { t } = useI18n()
const app = useAppStore()
const loading = ref(true), busy = ref(false), rulesLoading = ref(false), candidateLoading = ref(false)
const error = ref(''), dialogError = ref(''), bindingError = ref(''), ruleStatusError = ref('')
const presets = ref<RetryFilterPreset[]>([]), rules = ref<RetryFilterRule[]>([])
const selectedID = ref<number | null>(null), defaultID = ref<number | null>(null)
const selectedPreset = computed(() => presets.value.find(p => p.id === selectedID.value) || null)
const presetSearch = ref(''), presetDirty = ref(false)
const filteredPresets = computed(() => { const q = presetSearch.value.trim().toLowerCase(); return q ? presets.value.filter(p => `${p.name} ${p.description || ''}`.toLowerCase().includes(q)) : presets.value })
const presetForm = reactive({ name: '', description: '', enabled: true })
const editingRule = ref<number | 'new' | null>(null), draggedIndex = ref<number | null>(null)
const ruleForm = reactive<RuleDraft>(newRule())
const actions: RetryFilterAction[] = ['same_account_retry', 'switch_account', 'terminate', 'observe']
const deleteTarget = ref<{ kind: 'preset' | 'rule'; id: number; presetID?: number; name: string } | null>(null)
const bindingKinds: BindingKind[] = ['accounts', 'groups']
const bindingKind = ref<BindingKind>('groups'), bindingSearch = ref(''), candidatePage = ref(1), candidatePages = ref(1), candidateItems = ref<RetryFilterBindingCandidate[]>([])
const selectedAccountIDs = ref<Set<number>>(new Set()), selectedGroupIDs = ref<Set<number>>(new Set()), candidateCache = reactive<Record<BindingKind, Map<number, RetryFilterBindingCandidate>>>({ accounts: new Map(), groups: new Map() })
const initialAccountIDs = ref<Set<number>>(new Set()), initialGroupIDs = ref<Set<number>>(new Set())
const isGlobalDefault = ref(false)
const selectedIDs = computed(() => bindingKind.value === 'accounts' ? selectedAccountIDs.value : selectedGroupIDs.value)
const selectedCandidateItems = computed(() => Array.from(selectedIDs.value).map(id => candidateCache[bindingKind.value].get(id) || { id, name: `#${id}` }))

function newRule(): RuleDraft { return { name: '', description: '', enabled: true, priority: 0, match_mode: 'any', action: 'same_account_retry', statusText: '', keywordsText: '', codesText: '', pathsText: '', modelsText: '', retryCount: '', retryDelay: '', terminateStatus: '', terminateMessage: '', emptyRequestMode: 'none', emptyResponseMode: 'none' } }
const lines = (value: string) => value.split(/\r?\n/).map(v => v.trim()).filter(Boolean)
function parseStatusCodes(value: string): number[] | null {
  if (!value.trim()) return []
  const codes = new Set<number>()
  // Keep comma-separated entries intact so missing or malformed entries cannot be discarded.
  const entries = value.trim().replace(/\s*-\s*/g, '-').split(',')
  for (const entry of entries) {
    for (const token of entry.trim().split(/\s+/)) {
      const match = /^([1-5][0-9]{2})(?:-([1-5][0-9]{2}))?$/.exec(token)
      if (!match) return null
      const start = Number(match[1])
      const end = match[2] ? Number(match[2]) : start
      if (start > end) return null
      for (let code = start; code <= end; code++) codes.add(code)
    }
  }
  return Array.from(codes).sort((a, b) => a - b)
}
function formatStatusCodes(codes: number[]): string {
  const sorted = Array.from(new Set(codes)).sort((a, b) => a - b)
  const ranges: string[] = []
  for (let index = 0; index < sorted.length; index++) {
    const start = sorted[index]
    let end = start
    while (sorted[index + 1] === end + 1) end = sorted[++index]
    ranges.push(start === end ? String(start) : start + '-' + end)
  }
  return ranges.join(', ')
}
watch(() => ruleForm.statusText, () => { ruleStatusError.value = '' })
const failure = (e: unknown) => { const status = (e as { response?: { status?: number } })?.response?.status; return t(status === 409 ? 'admin.retryFilter.inUse' : 'admin.retryFilter.failed') }
const saved = () => app.showSuccess(t('admin.retryFilter.saved'))
const ruleCount = (id: number) => id === selectedID.value ? rules.value.length : '—'

async function load() {
  loading.value = true; error.value = ''
  try { const [list, defaults] = await Promise.all([retryFilterAPI.listPresets(), retryFilterAPI.getDefault()]); presets.value = list; defaultID.value = defaults.preset_id || null; if (!list.some(p => p.id === selectedID.value)) selectedID.value = list[0]?.id ?? null; await loadSelected() } catch (e) { error.value = failure(e) } finally { loading.value = false }
}
async function selectPreset(id: number) { if (id === selectedID.value) return; selectedID.value = id; await loadSelected() }
async function loadSelected() { const id = selectedID.value; if (!id) return; const p = selectedPreset.value; if (p) Object.assign(presetForm, { name: p.name, description: p.description || '', enabled: p.enabled }); presetDirty.value = false; editingRule.value = null; rulesLoading.value = true; try { rules.value = await retryFilterAPI.listRules(id); await loadPresetBindings(id) } catch (e) { error.value = failure(e) } finally { rulesLoading.value = false } }
watch(selectedID, (id, old) => { if (id && id !== old && !loading.value) void loadSelected() })
watch(bindingKind, () => { candidatePage.value = 1; void loadCandidates() })
async function createPreset() { busy.value = true; try { const p = await retryFilterAPI.createPreset({ name: t('admin.retryFilter.newRuleSet'), description: '', enabled: true }); presets.value.push(p); selectedID.value = p.id; await loadSelected(); saved() } catch (e) { error.value = failure(e) } finally { busy.value = false } }
async function savePreset() { if (!selectedID.value || !presetForm.name.trim()) { error.value = t('admin.retryFilter.nameRequired'); return } busy.value = true; error.value = ''; try { const p = await retryFilterAPI.updatePreset(selectedID.value, { name: presetForm.name.trim(), description: presetForm.description, enabled: presetForm.enabled }); const index = presets.value.findIndex(x => x.id === p.id); if (index >= 0) presets.value[index] = p; presetDirty.value = false; saved() } catch (e) { error.value = failure(e) } finally { busy.value = false } }
async function duplicatePreset() { const source = selectedPreset.value; if (!source) return; busy.value = true; try { const copy = await retryFilterAPI.createPreset({ name: `${source.name} (${t('admin.retryFilter.copySuffix')})`, description: source.description || '', enabled: source.enabled }); const sourceRules = await retryFilterAPI.listRules(source.id); for (const rule of sourceRules) await retryFilterAPI.createRule(copy.id, ruleToPayload(rule)); presets.value.push(copy); selectedID.value = copy.id; await loadSelected(); saved() } catch (e) { error.value = failure(e) } finally { busy.value = false } }

function beginRule(rule?: RetryFilterRule) { Object.assign(ruleForm, newRule(), rule ? draftFromRule(rule) : { priority: rules.value.length ? Math.max(...rules.value.map(r => r.priority)) + 1 : 0 }); editingRule.value = rule?.id ?? 'new'; dialogError.value = ''; ruleStatusError.value = '' }
function cancelRule() { editingRule.value = null; dialogError.value = ''; ruleStatusError.value = '' }
function draftFromRule(rule: RetryFilterRule): RuleDraft { return { ...newRule(), name: rule.name, description: rule.description || '', enabled: rule.enabled, priority: rule.priority, match_mode: rule.match_mode, action: rule.action, statusText: formatStatusCodes(rule.status_codes || []), keywordsText: (rule.keywords || []).join('\n'), codesText: (rule.upstream_codes || []).join('\n'), pathsText: (rule.upstream_code_paths || []).join('\n'), modelsText: (rule.models || []).join('\n'), retryCount: rule.retry_count_override == null ? '' : String(rule.retry_count_override), retryDelay: rule.retry_delay_ms == null ? '' : String(rule.retry_delay_ms), terminateStatus: rule.terminate_status_code == null ? '' : String(rule.terminate_status_code), terminateMessage: rule.terminate_message || '', emptyRequestMode: rule.empty_request_mode || 'none', emptyResponseMode: rule.empty_response_mode || 'none' } }
function ruleToPayload(rule: RetryFilterRule): RetryFilterRuleInput { return { name: rule.name, description: rule.description || '', enabled: rule.enabled, priority: rule.priority, match_mode: rule.match_mode, action: rule.action, status_codes: rule.status_codes || [], keywords: rule.keywords || [], upstream_codes: rule.upstream_codes || [], upstream_code_paths: rule.upstream_code_paths || [], models: rule.models || [], retry_count_override: rule.retry_count_override ?? null, retry_delay_ms: rule.retry_delay_ms ?? null, empty_request_mode: rule.empty_request_mode ?? 'none', empty_response_mode: rule.empty_response_mode ?? 'none', terminate_status_code: rule.terminate_status_code ?? null, terminate_message: rule.terminate_message ?? null } as RetryFilterRuleInput }
async function saveRule() {
  if (!selectedID.value || busy.value) return
  dialogError.value = ''
  ruleStatusError.value = ''
  const status_codes = parseStatusCodes(ruleForm.statusText)
  if (status_codes === null) {
    ruleStatusError.value = t('admin.retryFilter.invalidStatusCodes')
    return
  }
  const keywords = lines(ruleForm.keywordsText), upstream_codes = lines(ruleForm.codesText)
  const models = lines(ruleForm.modelsText), upstream_code_paths = lines(ruleForm.pathsText)
  if (!ruleForm.name.trim()) { dialogError.value = t('admin.retryFilter.nameRequired'); return }
  if (!status_codes.length && !keywords.length && !upstream_codes.length && !models.length && ruleForm.emptyRequestMode === 'none' && ruleForm.emptyResponseMode === 'none') {
    dialogError.value = t('admin.retryFilter.conditionRequired')
    return
  }
  if (models.some(m => m.includes('*') && (m === '*' || !m.endsWith('*') || m.slice(0, -1).includes('*')))) {
    dialogError.value = t('admin.retryFilter.invalidConditions')
    return
  }
  const retryCount = ruleForm.retryCount === '' ? null : Number(ruleForm.retryCount)
  const retryDelay = ruleForm.retryDelay === '' ? null : Number(ruleForm.retryDelay)
  if (retryCount != null && (!Number.isInteger(retryCount) || retryCount < 0 || retryCount > 50) || retryDelay != null && (!Number.isInteger(retryDelay) || retryDelay < 0 || retryDelay > 60000)) {
    dialogError.value = t('admin.retryFilter.invalidRetrySettings')
    return
  }
  const payload: RetryFilterRuleInput = {
    name: ruleForm.name.trim(), description: ruleForm.description, enabled: ruleForm.enabled,
    priority: ruleForm.priority, match_mode: ruleForm.match_mode, action: ruleForm.action,
    status_codes, keywords, upstream_codes, upstream_code_paths, models,
    retry_count_override: ruleForm.action === 'same_account_retry' ? retryCount : null,
    retry_delay_ms: ruleForm.action === 'same_account_retry' ? retryDelay : null,
    empty_request_mode: ruleForm.emptyRequestMode, empty_response_mode: ruleForm.emptyResponseMode,
    terminate_status_code: ruleForm.action === 'terminate' && ruleForm.terminateStatus ? Number(ruleForm.terminateStatus) : null,
    terminate_message: ruleForm.action === 'terminate' && ruleForm.terminateMessage ? ruleForm.terminateMessage : null
  }
  busy.value = true
  try {
    if (editingRule.value === 'new') await retryFilterAPI.createRule(selectedID.value, payload)
    else await retryFilterAPI.updateRule(selectedID.value, Number(editingRule.value), payload)
    editingRule.value = null
    rules.value = await retryFilterAPI.listRules(selectedID.value)
    saved()
  } catch (e) { dialogError.value = failure(e) } finally { busy.value = false }
}
async function toggleRule(rule: RetryFilterRule) { busy.value = true; try { await retryFilterAPI.updateRule(rule.preset_id, rule.id, { ...ruleToPayload(rule), enabled: !rule.enabled }); rules.value = await retryFilterAPI.listRules(rule.preset_id) } catch (e) { error.value = failure(e) } finally { busy.value = false } }
async function duplicateRule(rule: RetryFilterRule) { if (!selectedID.value) return; busy.value = true; try { await retryFilterAPI.createRule(selectedID.value, { ...ruleToPayload(rule), name: `${rule.name} (${t('admin.retryFilter.copySuffix')})`, priority: rule.priority + 1 }); rules.value = await retryFilterAPI.listRules(selectedID.value); saved() } catch (e) { error.value = failure(e) } finally { busy.value = false } }
function startDrag(index: number) { draggedIndex.value = index }
async function dropRule(index: number) { const from = draggedIndex.value; draggedIndex.value = null; if (from == null || from === index || !selectedID.value) return; const next = [...rules.value], [moved] = next.splice(from, 1); next.splice(index, 0, moved); next.forEach((r, i) => { r.priority = i }); rules.value = next; busy.value = true; try { await retryFilterAPI.reorderRules(selectedID.value, next.map(r => r.id)) } catch { try { for (const [i, r] of next.entries()) await retryFilterAPI.updateRule(selectedID.value, r.id, { ...ruleToPayload(r), priority: i }) } catch (e) { error.value = failure(e) } } finally { busy.value = false } }
function ruleSummary(rule: RetryFilterRule) {
  return [
    rule.status_codes?.length ? 'HTTP ' + formatStatusCodes(rule.status_codes) : '',
    rule.keywords?.length ? t('admin.retryFilter.keywords') + ': ' + rule.keywords.join(', ') : '',
    rule.upstream_codes?.length ? t('admin.retryFilter.upstreamCodes') + ': ' + rule.upstream_codes.join(', ') : '',
    rule.models?.length ? t('admin.retryFilter.models') + ': ' + rule.models.join(', ') : '',
    rule.empty_request_mode && rule.empty_request_mode !== 'none' ? t('admin.retryFilter.emptyRequest') : '',
    rule.empty_response_mode && rule.empty_response_mode !== 'none' ? t('admin.retryFilter.emptyResponse') : ''
  ].filter(Boolean)
}

async function loadPresetBindings(id: number) { selectedAccountIDs.value = new Set(); selectedGroupIDs.value = new Set(); initialAccountIDs.value = new Set(); initialGroupIDs.value = new Set(); isGlobalDefault.value = defaultID.value === id; bindingError.value = ''; try { const result = await retryFilterAPI.getPresetBindings(id); selectedAccountIDs.value = new Set(result.accounts || []); selectedGroupIDs.value = new Set(result.groups || []); initialAccountIDs.value = new Set(result.accounts || []); initialGroupIDs.value = new Set(result.groups || []) } catch { /* Older servers expose only per-entity bindings. */ } await loadCandidates() }
async function loadCandidates() { candidateLoading.value = true; bindingError.value = ''; try { const result = await retryFilterAPI.listBindingCandidates(bindingKind.value, { q: bindingSearch.value || undefined, page: candidatePage.value, page_size: 12, preset_id: selectedID.value || undefined }); candidateItems.value = result.items || []; candidatePages.value = Math.max(1, result.pages || Math.ceil((result.total || 0) / (result.page_size || 12))); for (const item of candidateItems.value) candidateCache[bindingKind.value].set(item.id, item) } catch { try { if (bindingKind.value === 'groups') { const result = await groupsAPI.list(candidatePage.value, 12, { search: bindingSearch.value || undefined }); candidateItems.value = result.items.map(g => ({ id: g.id, name: g.name, platform: g.platform, eligible: true })); candidatePages.value = Math.max(1, result.pages); for (const item of candidateItems.value) candidateCache.groups.set(item.id, item) } else { const result = await accountsAPI.list(candidatePage.value, 12, { search: bindingSearch.value || undefined, lite: 'true' }); candidateItems.value = result.items.map(a => ({ id: a.id, name: a.name, platform: a.platform, eligible: true })); candidatePages.value = Math.max(1, result.pages); for (const item of candidateItems.value) candidateCache.accounts.set(item.id, item) } } catch (e) { candidateItems.value = []; bindingError.value = failure(e) } } finally { candidateLoading.value = false } }
function changeCandidatePage(delta: number) { candidatePage.value = Math.min(Math.max(1, candidatePage.value + delta), candidatePages.value); void loadCandidates() }
function toggleCandidate(item: RetryFilterBindingCandidate) { const target = bindingKind.value === 'accounts' ? selectedAccountIDs : selectedGroupIDs; const next = new Set(target.value); next.has(item.id) ? next.delete(item.id) : next.add(item.id); target.value = next; candidateCache[bindingKind.value].set(item.id, item) }
async function saveBindings() { if (!selectedID.value) return; busy.value = true; bindingError.value = ''; try { await retryFilterAPI.setPresetBindings(selectedID.value, { accounts: Array.from(selectedAccountIDs.value), groups: Array.from(selectedGroupIDs.value), global: isGlobalDefault.value }); defaultID.value = isGlobalDefault.value ? selectedID.value : (defaultID.value === selectedID.value ? null : defaultID.value); initialAccountIDs.value = new Set(selectedAccountIDs.value); initialGroupIDs.value = new Set(selectedGroupIDs.value); saved() } catch (e) { bindingError.value = failure(e) } finally { busy.value = false } }
async function confirmDelete() { const target = deleteTarget.value; if (!target) return; busy.value = true; dialogError.value = ''; try { if (target.kind === 'preset') { await retryFilterAPI.deletePreset(target.id); presets.value = presets.value.filter(p => p.id !== target.id); selectedID.value = presets.value[0]?.id ?? null; if (selectedID.value) await loadSelected() } else { await retryFilterAPI.deleteRule(target.presetID!, target.id); rules.value = rules.value.filter(r => r.id !== target.id) } deleteTarget.value = null; saved() } catch (e) { dialogError.value = failure(e) } finally { busy.value = false } }
onMounted(load)

const RuleEditor = defineComponent({
  props: {
    draft: { type: Object, required: true },
    error: { type: String, default: '' },
    statusError: { type: String, default: '' }
  },
  emits: ['save'],
  setup(props, { emit }) {
    const d = props.draft as RuleDraft
    const statusInput = ref<HTMLTextAreaElement | null>(null)
    const textField = (labelKey: string, key: keyof RuleDraft, kind: 'input' | 'textarea', placeholder = '', hintKey = '') => h('label', { class: 'block min-w-0' }, [
      h('span', { class: 'input-label' }, t('admin.retryFilter.' + labelKey)),
      h(kind, {
        class: 'input', name: key, placeholder, rows: kind === 'textarea' ? 2 : undefined,
        'aria-describedby': hintKey ? 'retry-filter-' + key + '-hint' : undefined,
        value: d[key] as string,
        onInput: (e: Event) => { d[key] = (e.target as HTMLInputElement | HTMLTextAreaElement).value as never }
      }),
      hintKey ? h('span', { id: 'retry-filter-' + key + '-hint', class: 'mt-1 block text-xs leading-relaxed text-gray-500' }, t('admin.retryFilter.' + hintKey)) : null
    ])
    const statusCodesField = () => h('label', { class: 'block min-w-0' }, [
      h('span', { class: 'input-label' }, t('admin.retryFilter.statusCodes')),
      h('textarea', {
        ref: statusInput, id: 'retry-filter-status-codes', name: 'statusText', rows: 2, class: 'input resize-y font-mono',
        placeholder: t('admin.retryFilter.statusPlaceholder'), value: d.statusText,
        'aria-invalid': !!props.statusError,
        'aria-describedby': 'retry-filter-status-hint' + (props.statusError ? ' retry-filter-status-error' : ''),
        onInput: (e: Event) => { d.statusText = (e.target as HTMLTextAreaElement).value }
      }),
      h('span', { id: 'retry-filter-status-hint', class: 'mt-1 block text-xs leading-relaxed text-gray-500' }, t('admin.retryFilter.statusHint')),
      props.statusError ? h('span', { id: 'retry-filter-status-error', role: 'alert', class: 'mt-2 block text-sm text-red-600 dark:text-red-400' }, props.statusError) : null
    ])
    const numberField = (key: keyof RuleDraft, min: number, max: number, labelKey: string) => h('label', { class: 'block min-w-0' }, [h('span', { class: 'input-label' }, t('admin.retryFilter.' + labelKey)), h('input', { class: 'input', name: key, type: 'number', min, max, step: 1, value: d[key] as string, placeholder: t('admin.retryFilter.keepOriginal'), onInput: (e: Event) => { d[key] = (e.target as HTMLInputElement).value as never } })])
    const selectField = (labelKey: string, key: 'match_mode' | 'action' | 'emptyRequestMode' | 'emptyResponseMode', options: SelectOption[]) => h('div', { class: 'min-w-0' }, [
      h('label', { class: 'input-label', for: 'retry-filter-' + key }, t('admin.retryFilter.' + labelKey)),
      h(Select, {
        id: 'retry-filter-' + key, ariaLabel: t('admin.retryFilter.' + labelKey),
        modelValue: d[key], options, searchable: false,
        'onUpdate:modelValue': (value: SelectOption['value']) => { d[key] = value as never }
      })
    ])
    const emptyOptions = (values: string[], response = false): SelectOption[] => values.map(value => ({
      value, label: t('admin.retryFilter.emptyModes.' + (response && value === 'both' ? 'responseBoth' : value))
    }))
    return () => h('form', {
      id: 'retry-filter-rule-editor', class: 'min-w-0 space-y-5',
      onSubmit: async (event: Event) => {
        event.preventDefault()
        emit('save')
        await nextTick()
        if (props.statusError) statusInput.value?.focus()
      }
    }, [
      props.error ? h('p', { role: 'alert', class: 'rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300' }, props.error) : null,
      h('div', { class: 'grid items-end gap-4 sm:grid-cols-[minmax(0,1fr)_120px_auto]' }, [
        h('label', { class: 'block min-w-0' }, [h('span', { class: 'input-label' }, t('admin.retryFilter.name')), h('input', { class: 'input', name: 'name', value: d.name, maxlength: 200, onInput: (e: Event) => { d.name = (e.target as HTMLInputElement).value } })]),
        h('label', { class: 'block min-w-0' }, [h('span', { class: 'input-label' }, t('admin.retryFilter.priority')), h('input', { class: 'input', name: 'priority', type: 'number', value: d.priority, onInput: (e: Event) => { d.priority = Number((e.target as HTMLInputElement).value) } })]),
        h('div', { class: 'flex items-center gap-3 pb-2' }, [
          h('span', { class: 'text-sm font-medium text-gray-700 dark:text-gray-300' }, t('admin.retryFilter.enabled')),
          h(Toggle, { 'aria-label': t('admin.retryFilter.enabled'), modelValue: d.enabled, 'onUpdate:modelValue': (value: boolean) => { d.enabled = value } })
        ])
      ]),
      h('p', { class: 'text-xs leading-relaxed text-gray-500' }, t('admin.retryFilter.priorityHint')),
      h('fieldset', { class: 'min-w-0 space-y-4 rounded-xl border border-gray-200 p-3 sm:p-4 dark:border-dark-700' }, [
        h('legend', { class: 'px-2 text-sm font-semibold' }, t('admin.retryFilter.conditions')),
        h('p', { class: 'text-xs leading-relaxed text-gray-500' }, t('admin.retryFilter.conditionHint')),
        statusCodesField(),
        h('div', { class: 'grid gap-4 md:grid-cols-2' }, [
          textField('keywords', 'keywordsText', 'textarea', 'overloaded'),
          textField('models', 'modelsText', 'textarea', t('admin.retryFilter.modelPlaceholder'), 'modelHint'),
          textField('upstreamCodes', 'codesText', 'textarea', 'rate_limit_exceeded'),
          textField('codePaths', 'pathsText', 'textarea', 'error.code\ncode', 'pathHint')
        ]),
        selectField('matchMode', 'match_mode', [{ value: 'any', label: t('admin.retryFilter.any') }, { value: 'all', label: t('admin.retryFilter.all') }]),
        h('div', { class: 'grid gap-4 md:grid-cols-2' }, [
          selectField('emptyRequest', 'emptyRequestMode', emptyOptions(['none', 'body', 'structured', 'both'])),
          selectField('emptyResponse', 'emptyResponseMode', emptyOptions(['none', 'completed', 'interrupted', 'both'], true))
        ])
      ]),
      h('div', { class: 'grid gap-3 md:grid-cols-2' }, [
        selectField('action', 'action', actions.map(action => ({ value: action, label: t(`admin.retryFilter.actions.${action}`) }))),
        h('p', { class: 'input-hint', id: 'retry-filter-action-hint' }, t(`admin.retryFilter.actionHints.${d.action}`))
      ]),
      d.action === 'same_account_retry' ? h('div', { class: 'grid gap-3 md:grid-cols-2' }, [numberField('retryCount', 0, 50, 'retryCount'), numberField('retryDelay', 0, 60000, 'retryDelay')]) : null,
      d.action === 'terminate' ? h('div', { class: 'grid gap-3 md:grid-cols-2' }, [numberField('terminateStatus', 100, 599, 'terminateStatus'), textField('terminateMessage', 'terminateMessage', 'textarea')]) : null,
      textField('notes', 'description', 'textarea')
    ])
  }
})
</script>
