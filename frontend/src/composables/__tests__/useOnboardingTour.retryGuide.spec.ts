import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingStore } from '@/stores/onboarding'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import type { User } from '@/types'
import { useOnboardingTour } from '../useOnboardingTour'
import RetryFilterGuide from '@/components/admin/RetryFilterGuide.vue'

const controls = vi.hoisted(() => ({ fail: false, inactive: false, destroyed: null as (() => void) | null }))
vi.mock('driver.js', () => ({
  driver: (config: { onDestroyed: () => void }) => {
    if (controls.fail) throw Error('tour unavailable')
    let active = false
    const destroy = () => { active = false; config.onDestroyed() }
    controls.destroyed = destroy
    return { drive: () => { active = !controls.inactive }, destroy, isActive: () => active }
  }
}))
vi.mock('@/components/Guide/steps', () => ({ getAdminSteps: () => [], getUserSteps: () => [] }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const Layout = defineComponent({
  components: { RetryFilterGuide },
  setup() { return useOnboardingTour({ storageKey: 'admin_guide', autoStart: true }) },
  template: '<div><RetryFilterGuide /></div>'
})

function mountLayout({ seen = false } = {}) {
  const pinia = createPinia()
  useAuthStore(pinia).user = { id: 7, role: 'admin' } as User
  useAdminComplianceStore(pinia).reset()
  if (seen) localStorage.setItem('admin_guide_7_admin_v4_interactive', 'true')
  return { wrapper: mount(Layout, { global: { plugins: [pinia], stubs: { Teleport: true } } }), onboarding: useOnboardingStore(pinia) }
}

enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers(); controls.fail = false; controls.inactive = false; controls.destroyed = null; localStorage.clear() })

describe('global onboarding with retry filter first visit', () => {
  it('keeps page help hidden during the global delay and active tour, then releases it on close', async () => {
    vi.useFakeTimers()
    const { wrapper, onboarding } = mountLayout()
    await flushPromises()
    expect(onboarding.tourBlocking).toBe(true)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(onboarding.tourBlocking).toBe(true)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    controls.destroyed!()
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    expect(localStorage.getItem('sub2api_retry_filter_guide_v1:7')).toBeNull()
  })

  it('opens page help immediately when the global guide has already been dismissed', async () => {
    const { wrapper, onboarding } = mountLayout({ seen: true })
    await flushPromises()
    expect(onboarding.tourBlocking).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  })

  it('clears the scheduled state if the page unmounts before the timer starts', async () => {
    vi.useFakeTimers()
    const { wrapper, onboarding } = mountLayout()
    await flushPromises()
    expect(onboarding.tourBlocking).toBe(true)
    wrapper.unmount()
    expect(onboarding.tourBlocking).toBe(false)
    await vi.advanceTimersByTimeAsync(1000)
    expect(controls.destroyed).toBeNull()
  })

  it('clears the pending state when explicit tour construction fails', async () => {
    controls.fail = true
    const { wrapper, onboarding } = mountLayout({ seen: true })
    await expect(wrapper.vm.startTour()).rejects.toThrow('tour unavailable')
    expect(onboarding.tourBlocking).toBe(false)
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  })

  it('does not block page help when a driver has no active step', async () => {
    controls.inactive = true
    const { wrapper, onboarding } = mountLayout({ seen: true })
    await wrapper.vm.startTour()
    await flushPromises()
    expect(onboarding.tourBlocking).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    controls.destroyed!()
  })
})
