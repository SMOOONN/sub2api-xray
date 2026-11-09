<template>
  <form id="retry-filter-rule-editor" class="min-w-0" novalidate @submit.prevent="submit">
    <div class="mb-5 flex gap-6 border-b border-gray-200 dark:border-dark-700" role="tablist" :aria-label="t('admin.retryFilter.ruleSteps')" @keydown="navigateSteps">
      <button v-for="(item, index) in steps" :id="`retry-rule-tab-${item}`" :key="item" type="button" role="tab" :aria-controls="`retry-rule-panel-${item}`" :aria-selected="step === item" :tabindex="step === item ? 0 : -1" :disabled="busy" class="flex items-center gap-2 border-b-2 px-1 pb-3 text-sm" :class="step === item ? 'border-primary-600 font-semibold text-primary-700 dark:text-primary-300' : 'border-transparent text-gray-500'" @click="emit('step', item)">
        <span class="flex h-5 w-5 items-center justify-center rounded bg-gray-100 text-xs dark:bg-dark-700">{{ index + 1 }}</span>
        {{ t(`admin.retryFilter.${item === 'conditions' ? 'conditions' : 'action'}`) }}
      </button>
    </div>
    <p v-if="error" role="alert" class="mb-4 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>

    <section v-show="step === 'conditions'" id="retry-rule-panel-conditions" role="tabpanel" aria-labelledby="retry-rule-tab-conditions" class="space-y-5">
      <label class="block"><span class="input-label">{{ t('admin.retryFilter.name') }}</span><input v-model="draft.name" name="name" class="input" maxlength="200" :disabled="busy" /></label>
      <fieldset :disabled="busy" class="space-y-3">
        <legend class="input-label">{{ t('admin.retryFilter.matchMode') }}</legend>
        <div class="flex flex-wrap gap-x-5 gap-y-2">
          <label v-for="mode in ['any', 'all'] as const" :key="mode" class="flex items-center gap-2 text-sm"><input v-model="draft.match_mode" type="radio" name="match_mode" :value="mode" class="text-primary-600 focus:ring-primary-500" />{{ t(`admin.retryFilter.${mode}`) }}</label>
        </div>
      </fieldset>
      <div>
        <p class="input-label">{{ t('admin.retryFilter.chooseConditions') }}</p>
        <p class="mb-3 text-xs text-gray-500">{{ t('admin.retryFilter.conditionHint') }}</p>
        <div class="flex flex-wrap gap-2">
          <button v-for="item in dimensions" :key="item.key" type="button" :data-dimension="item.key" :aria-pressed="activeDimensions.has(item.key)" :disabled="busy" class="flex items-center gap-1.5 rounded-lg border px-3 py-2 text-xs" :class="activeDimensions.has(item.key) ? 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'border-gray-200 text-gray-600 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700'" @click="toggleDimension(item.key)"><Icon :name="activeDimensions.has(item.key) ? 'check' : 'plus'" size="xs" />{{ t(`admin.retryFilter.${item.label}`) }}</button>
        </div>
      </div>

      <fieldset v-for="item in visibleDimensions" :key="item.key" :disabled="busy" class="min-w-0 space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <legend class="px-2 text-sm font-medium">{{ t(`admin.retryFilter.${item.label}`) }}</legend>
        <template v-if="item.key === 'statusText'">
          <label for="retry-filter-status-codes" class="sr-only">{{ t('admin.retryFilter.statusCodes') }}</label>
          <textarea id="retry-filter-status-codes" v-model="draft.statusText" name="statusText" rows="2" class="input font-mono" :placeholder="t('admin.retryFilter.statusPlaceholder')" :aria-invalid="!!statusError" :aria-describedby="'retry-filter-status-hint' + (statusError ? ' retry-filter-status-error' : '')"></textarea>
          <p id="retry-filter-status-hint" class="text-xs leading-relaxed text-gray-500">{{ t('admin.retryFilter.statusHint') }}</p>
          <p v-if="statusError" id="retry-filter-status-error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ statusError }}</p>
        </template>
        <template v-else-if="item.key === 'emptyRequestMode' || item.key === 'emptyResponseMode'">
          <Select :id="`retry-filter-${item.key}`" :model-value="draft[item.key]" :aria-label="t(`admin.retryFilter.${item.label}`)" :options="emptyOptions(item.key)" :searchable="false" :disabled="busy" @update:model-value="setEmptyMode(item.key, $event)" />
        </template>
        <template v-else>
          <label :for="`retry-filter-${item.key}`" class="sr-only">{{ t(`admin.retryFilter.${item.label}`) }}</label>
          <textarea :id="`retry-filter-${item.key}`" v-model="draft[item.key]" :name="item.key" rows="2" class="input" :placeholder="item.key === 'modelsText' ? t('admin.retryFilter.modelPlaceholder') : ''"></textarea>
          <p v-if="item.key === 'modelsText'" class="text-xs leading-relaxed text-gray-500">{{ t('admin.retryFilter.modelHint') }}</p>
          <details v-if="item.key === 'codesText'" :open="!!draft.pathsText" class="text-xs text-gray-500">
            <summary class="cursor-pointer py-1">{{ t('admin.retryFilter.codePaths') }}</summary>
            <label for="retry-filter-pathsText" class="sr-only">{{ t('admin.retryFilter.codePaths') }}</label>
            <textarea id="retry-filter-pathsText" v-model="draft.pathsText" name="pathsText" rows="2" class="input mt-2 font-mono" placeholder="error.type&#10;error.code"></textarea>
            <p class="mt-2 leading-relaxed">{{ t('admin.retryFilter.pathHint') }}</p>
          </details>
        </template>
      </fieldset>
    </section>

    <section v-show="step === 'action'" id="retry-rule-panel-action" role="tabpanel" aria-labelledby="retry-rule-tab-action" class="space-y-5">
      <fieldset :disabled="busy">
        <legend class="input-label">{{ t('admin.retryFilter.onMatch') }}</legend>
        <div class="grid gap-3 sm:grid-cols-2">
          <label v-for="action in actions" :key="action" class="flex cursor-pointer items-start gap-3 rounded-xl border p-4" :class="draft.action === action ? 'border-primary-400 bg-primary-50 dark:border-primary-600 dark:bg-primary-900/20' : 'border-gray-200 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700'">
            <input v-model="draft.action" type="radio" name="action" :value="action" class="mt-1 text-primary-600 focus:ring-primary-500" />
            <span class="min-w-0 text-sm font-medium">{{ t(`admin.retryFilter.actions.${action}`) }}<span class="mt-1 block text-xs font-normal leading-relaxed text-gray-500 dark:text-gray-400">{{ t(`admin.retryFilter.actionShortHints.${action}`) }}</span></span>
          </label>
        </div>
      </fieldset>
      <p id="retry-filter-action-hint" class="rounded-lg bg-gray-50 p-3 text-xs leading-relaxed text-gray-500 dark:bg-dark-900/50 dark:text-gray-400">{{ t(`admin.retryFilter.actionHints.${draft.action}`) }}</p>
      <fieldset v-if="draft.action === 'same_account_retry'" :disabled="busy" class="grid gap-4 sm:grid-cols-2">
        <label><span class="input-label">{{ t('admin.retryFilter.retryCount') }}</span><input v-model="draft.retryCount" class="input" name="retryCount" type="number" min="0" max="50" step="1" :placeholder="t('admin.retryFilter.keepOriginal')" /></label>
        <label><span class="input-label">{{ t('admin.retryFilter.retryDelay') }}</span><input v-model="draft.retryDelay" class="input" name="retryDelay" type="number" min="0" max="60000" step="1" :placeholder="t('admin.retryFilter.keepOriginal')" /></label>
      </fieldset>
      <fieldset v-if="draft.action === 'terminate'" :disabled="busy" class="space-y-4">
        <label class="block"><span class="input-label">{{ t('admin.retryFilter.terminateStatus') }}</span><input v-model="draft.terminateStatus" name="terminateStatus" class="input" type="number" min="100" max="599" step="1" :placeholder="t('admin.retryFilter.keepOriginal')" /></label>
        <label class="block"><span class="input-label">{{ t('admin.retryFilter.terminateMessage') }}</span><textarea v-model="draft.terminateMessage" name="terminateMessage" class="input" rows="2"></textarea></label>
      </fieldset>
      <label class="block"><span class="input-label">{{ t('admin.retryFilter.notes') }}</span><textarea v-model="draft.description" name="description" rows="2" class="input" :disabled="busy"></textarea></label>
      <div class="flex items-center justify-between gap-4"><span class="text-sm font-medium">{{ t('admin.retryFilter.enable') }}</span><Toggle v-model="draft.enabled" :aria-label="t('admin.retryFilter.enable')" :disabled="busy" /></div>
      <details class="text-xs text-gray-500"><summary class="cursor-pointer py-2">{{ t('admin.retryFilter.priority') }}</summary><label class="mt-2 block"><span class="input-label">{{ t('admin.retryFilter.priority') }}</span><input v-model.number="draft.priority" name="priority" type="number" step="1" class="input" :disabled="busy" /></label><p class="mt-2">{{ t('admin.retryFilter.priorityHint') }}</p></details>
    </section>
  </form>
</template>

<script lang="ts">
import type { RetryFilterAction, RetryFilterEmptyRequestMode, RetryFilterEmptyResponseMode } from '@/api/admin/retryFilter'

export type RuleDraft = {
  name: string; description: string; enabled: boolean; priority: number; match_mode: 'any' | 'all'; action: RetryFilterAction
  statusText: string; keywordsText: string; codesText: string; pathsText: string; modelsText: string
  retryCount: string | number; retryDelay: string | number; terminateStatus: string | number; terminateMessage: string
  emptyRequestMode: RetryFilterEmptyRequestMode; emptyResponseMode: RetryFilterEmptyResponseMode
}
export type RuleStep = 'conditions' | 'action'
</script>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'

const draft = defineModel<RuleDraft>('draft', { required: true })
const props = defineProps<{ step: RuleStep; error: string; statusError: string; busy: boolean }>()
const emit = defineEmits<{ submit: []; step: [step: RuleStep] }>()
const { t } = useI18n()
const steps: RuleStep[] = ['conditions', 'action']
const actions: RetryFilterAction[] = ['same_account_retry', 'switch_account', 'terminate', 'observe']
const dimensions = [
  { key: 'statusText', label: 'statusCodes' }, { key: 'keywordsText', label: 'keywords' },
  { key: 'codesText', label: 'upstreamCodes' }, { key: 'modelsText', label: 'models' },
  { key: 'emptyRequestMode', label: 'emptyRequest' }, { key: 'emptyResponseMode', label: 'emptyResponse' }
] as const
type Dimension = typeof dimensions[number]['key']
const activeDimensions = ref(new Set<Dimension>(dimensions.filter(({ key }) => {
  const value = draft.value[key]
  return key === 'emptyRequestMode' || key === 'emptyResponseMode' ? value !== 'none' : !!value || (key === 'codesText' && !!draft.value.pathsText)
}).map(item => item.key)))
if (!activeDimensions.value.size) activeDimensions.value.add('statusText')
const visibleDimensions = computed(() => dimensions.filter(item => activeDimensions.value.has(item.key)))
function toggleDimension(key: Dimension) {
  if (activeDimensions.value.has(key)) {
    activeDimensions.value.delete(key)
    if (key === 'emptyRequestMode' || key === 'emptyResponseMode') draft.value[key] = 'none'
    else draft.value[key] = ''
    if (key === 'codesText') draft.value.pathsText = ''
  } else {
    activeDimensions.value.add(key)
    if (key === 'emptyRequestMode') draft.value[key] = 'body'
    if (key === 'emptyResponseMode') draft.value[key] = 'completed'
  }
}
function emptyOptions(key: 'emptyRequestMode' | 'emptyResponseMode'): SelectOption[] {
  const response = key === 'emptyResponseMode'
  return (response ? ['none', 'completed', 'interrupted', 'both'] : ['none', 'body', 'structured', 'both']).map(value => ({ value, label: t(`admin.retryFilter.emptyModes.${response && value === 'both' ? 'responseBoth' : value}`) }))
}
function setEmptyMode(key: Dimension, value: SelectOption['value']) {
  if (key === 'emptyRequestMode' && ['none', 'body', 'structured', 'both'].includes(String(value))) draft.value.emptyRequestMode = value as RetryFilterEmptyRequestMode
  if (key === 'emptyResponseMode' && ['none', 'completed', 'interrupted', 'both'].includes(String(value))) draft.value.emptyResponseMode = value as RetryFilterEmptyResponseMode
}
function submit(event: Event) {
  if (props.step === 'action') {
    const form = event.currentTarget as HTMLFormElement
    // A number input can expose an empty value for incomplete text such as "1e".
    // Preserve the browser's badInput distinction before optional blanks become null.
    const invalid = [...form.querySelectorAll<HTMLInputElement>('input[type="number"]')].find(input => !input.validity.valid)
    if (invalid) {
      invalid.closest('details')?.setAttribute('open', '')
      invalid.reportValidity()
      invalid.focus()
      return
    }
  }
  emit('submit')
}
async function navigateSteps(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key) || props.busy) return
  event.preventDefault()
  emit('step', event.key === 'Home' ? 'conditions' : event.key === 'End' ? 'action' : props.step === 'conditions' ? 'action' : 'conditions')
  await nextTick()
  document.getElementById(`retry-rule-tab-${props.step}`)?.focus()
}
watch(() => props.statusError, async error => {
  if (!error) return
  activeDimensions.value.add('statusText')
  await nextTick()
  document.getElementById('retry-filter-status-codes')?.focus()
})
</script>
