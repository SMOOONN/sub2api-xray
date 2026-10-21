<template>
  <button v-if="auth.isAdmin" type="button" class="btn btn-secondary" @click="openGuide">
    {{ t('admin.retryFilter.guide.open') }}
  </button>

  <BaseDialog :show="showGuide" :title="t('admin.retryFilter.guide.title')" @close="closeGuide">
    <div class="space-y-5" aria-live="polite" aria-atomic="true">
      <div class="flex items-center justify-between gap-4">
        <p class="text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.retryFilter.guide.progress', { current: currentStep + 1, total: steps.length }) }}
        </p>
        <div class="flex gap-1.5" aria-hidden="true">
          <span
            v-for="(_, index) in steps"
            :key="index"
            class="h-1.5 w-5 rounded-full"
            :class="index === currentStep ? 'bg-primary-500' : 'bg-gray-200 dark:bg-dark-600'"
          ></span>
        </div>
      </div>
      <section
        data-test="retry-filter-guide-step"
        :data-step="currentStep + 1"
        class="min-h-44 space-y-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-900/40"
      >
        <h4 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(step.title) }}</h4>
        <p class="break-words text-sm leading-7 text-gray-600 dark:text-dark-300">{{ t(step.body) }}</p>
      </section>
      <details class="rounded-xl border border-gray-200 px-4 py-3 text-sm dark:border-dark-700" data-test="retry-filter-guide-extra">
        <summary class="cursor-pointer font-medium text-gray-700 dark:text-dark-200">{{ t('admin.retryFilter.guide.extraTitle') }}</summary>
        <div class="mt-4 space-y-4 text-gray-600 dark:text-dark-300">
          <section v-for="note in extraNotes" :key="note.title" class="space-y-1">
            <h4 class="font-medium text-gray-900 dark:text-white">{{ t(note.title) }}</h4>
            <p class="break-words leading-relaxed">{{ t(note.body) }}</p>
          </section>
        </div>
      </details>
      <p class="text-xs leading-relaxed text-gray-500 dark:text-dark-400">{{ t('admin.retryFilter.guide.reopenHint') }}</p>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-3">
        <button type="button" class="btn btn-secondary" data-test="retry-filter-guide-dismiss" @click="closeGuide">
          {{ t('admin.retryFilter.guide.dismiss') }}
        </button>
        <div class="flex gap-2">
          <button
            type="button"
            class="btn btn-secondary"
            data-test="retry-filter-guide-previous"
            :disabled="currentStep === 0"
            @click="currentStep--"
          >
            {{ t('admin.retryFilter.guide.previous') }}
          </button>
          <button type="button" class="btn btn-primary" data-dialog-autofocus data-test="retry-filter-guide-next" @click="nextStep">
            {{ currentStep === steps.length - 1 ? t('admin.retryFilter.guide.finish') : t('admin.retryFilter.guide.next') }}
          </button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingStore } from '@/stores/onboarding'
import { useAdminComplianceStore } from '@/stores/adminCompliance'

const { t } = useI18n()
const auth = useAuthStore()
const onboarding = useOnboardingStore()
const compliance = useAdminComplianceStore()
const guideRequested = ref(false)
const showGuide = computed(() => guideRequested.value && !onboarding.tourBlocking && !compliance.shouldShow && !compliance.loading)
const currentStep = ref(0)
const storageKey = computed(() => auth.isAdmin && auth.user?.id ? `sub2api_retry_filter_guide_v1:${auth.user.id}` : null)
const steps = [
  { title: 'admin.retryFilter.guide.enableTitle', body: 'admin.retryFilter.guide.enableBody' },
  { title: 'admin.retryFilter.guide.ruleSetTitle', body: 'admin.retryFilter.guide.ruleSetBody' },
  { title: 'admin.retryFilter.guide.bindingTitle', body: 'admin.retryFilter.guide.bindingBody' },
  { title: 'admin.retryFilter.guide.retryTitle', body: 'admin.retryFilter.guide.retryBody' },
  { title: 'admin.retryFilter.guide.orderTitle', body: 'admin.retryFilter.guide.orderBody' }
]
const extraNotes = [
  { title: 'admin.retryFilter.guide.precedenceTitle', body: 'admin.retryFilter.guide.precedenceBody' },
  { title: 'admin.retryFilter.guide.wsTitle', body: 'admin.retryFilter.guide.wsBody' },
  { title: 'admin.retryFilter.guide.breakerTitle', body: 'admin.retryFilter.guide.breakerBody' }
]
const step = computed(() => steps[currentStep.value])

function openGuide() {
  currentStep.value = 0
  guideRequested.value = true
}

function showForFirstVisit() {
  if (!storageKey.value) return
  try {
    if (localStorage.getItem(storageKey.value) === 'seen') return
  } catch {
    // Reading help remains available when the browser blocks local storage.
  }
  openGuide()
}

function closeGuide() {
  guideRequested.value = false
  if (!storageKey.value) return
  try {
    localStorage.setItem(storageKey.value, 'seen')
  } catch {
    // A storage failure must not prevent closing the guide.
  }
}

function nextStep() {
  if (currentStep.value === steps.length - 1) closeGuide()
  else currentStep.value++
}

onMounted(showForFirstVisit)
watch(storageKey, () => {
  guideRequested.value = false
  showForFirstVisit()
})
</script>
