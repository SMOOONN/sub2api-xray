import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingStore } from '@/stores/onboarding'
import { useAdminComplianceStore } from '@/stores/adminCompliance'
import type { User } from '@/types'
import zh from '@/i18n/locales/zh/admin/retryFilter'
import en from '@/i18n/locales/en/admin/retryFilter'
import RetryFilterGuide from '../RetryFilterGuide.vue'

// Vitest uses the runtime-only i18n alias without Vite's JIT build flag.
vi.mock('vue-i18n', async () => {
  const { createRequire } = await import('node:module')
  const require = createRequire(import.meta.url)
  return vi.importActual(require.resolve('vue-i18n/dist/vue-i18n.esm-bundler.js'))
})

const key = (id: number) => `sub2api_retry_filter_guide_v1:${id}`
const actor = (id: number, role = 'admin') => ({ id, role } as User)

function mountGuide({ id = 7, role = 'admin', locale = 'zh', authenticated = true, tourPending = false, complianceRequired = false } = {}) {
  const pinia = createPinia()
  const auth = useAuthStore(pinia)
  auth.user = authenticated ? actor(id, role) : null
  const onboarding = useOnboardingStore(pinia)
  onboarding.setTourPending(tourPending)
  const compliance = useAdminComplianceStore(pinia)
  if (complianceRequired) compliance.requireAcknowledgement()
  const wrapper = mount(RetryFilterGuide, {
    attachTo: document.body,
    global: {
      plugins: [pinia, createI18n({ legacy: false, locale, messages: { zh: { admin: zh }, en: { admin: en } } })],
      stubs: { Teleport: true }
    }
  })
  return { wrapper, auth, onboarding, compliance }
}

enableAutoUnmount(afterEach)
beforeEach(() => localStorage.clear())
afterEach(() => vi.restoreAllMocks())

describe('retry filter page guide', () => {
  it('waits for an already scheduled global tour, then shows the pending first visit', async () => {
    const { wrapper, onboarding } = mountGuide({ tourPending: true })
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(localStorage.getItem(key(7))).toBeNull()
    onboarding.setTourPending(false)
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  })

  it('defers the guide behind compliance and resumes the same tip without marking it seen', async () => {
    const { wrapper, compliance } = mountGuide({ complianceRequired: true })
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    compliance.reset()
    await flushPromises()
    await wrapper.get('[data-test="retry-filter-guide-next"]').trigger('click')
    compliance.requireAcknowledgement()
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(localStorage.getItem(key(7))).toBeNull()
    compliance.reset()
    await flushPromises()
    expect(wrapper.get('[data-test="retry-filter-guide-step"]').attributes('data-step')).toBe('2')
  })

  it('shows five tips one at a time, supports going back, and remembers completion', async () => {
    const { wrapper } = mountGuide()
    await flushPromises()
    const titles = [zh.retryFilter.guide.enableTitle, zh.retryFilter.guide.ruleSetTitle, zh.retryFilter.guide.bindingTitle, zh.retryFilter.guide.retryTitle, zh.retryFilter.guide.orderTitle]
    const extra = wrapper.get('[data-test="retry-filter-guide-extra"]')
    expect(extra.element.tagName).toBe('DETAILS')
    expect(extra.attributes('open')).toBeUndefined()
    expect(extra.get('summary').text()).toBe('补充说明（特殊情况）')
    expect(extra.text()).toContain('WebSocket')
    expect(extra.text()).toContain('Alpha Search')
    expect(extra.text()).toContain('第一份可用的规则集')
    expect(wrapper.get('[data-test="retry-filter-guide-previous"]').attributes('disabled')).toBeDefined()
    expect(localStorage.getItem(key(7))).toBeNull()
    for (let index = 0; index < titles.length; index++) {
      const step = wrapper.get('[data-test="retry-filter-guide-step"]')
      expect(step.attributes('data-step')).toBe(String(index + 1))
      expect(step.get('h4').text()).toBe(titles[index])
      expect(step.findAll('p')).toHaveLength(1)
      expect(wrapper.text()).toContain(`第 ${index + 1} / 5 条`)
      expect(step.text()).not.toContain('WebSocket')
      expect(step.text()).not.toContain('熔断器')
      if (index === 0) {
        expect(step.text()).toContain('打开“池模式”')
        expect(step.text()).toContain('OAuth 账号暂不支持')
      }
      if (index === 2) {
        expect(step.text()).toContain('A 账号属于 X 分组')
        expect(step.text()).toContain('保存应用范围')
      }
      if (index === 3) {
        expect(step.text()).toContain('总共最多请求 3 次')
        expect(step.text()).toContain('等 1 秒再试')
      }
      if (index === 4) {
        expect(step.text()).toContain('重试监控')
        await wrapper.get('[data-test="retry-filter-guide-previous"]').trigger('click')
        expect(wrapper.get('[data-test="retry-filter-guide-step"] h4').text()).toBe(titles[3])
        await wrapper.get('[data-test="retry-filter-guide-next"]').trigger('click')
        expect(wrapper.get('[data-test="retry-filter-guide-next"]').text()).toBe('我知道了')
      }
      await wrapper.get('[data-test="retry-filter-guide-next"]').trigger('click')
    }
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(localStorage.getItem(key(7))).toBe('seen')
    wrapper.unmount()
    const revisited = mountGuide().wrapper
    await flushPromises()
    expect(revisited.find('[role="dialog"]').exists()).toBe(false)
  })

  it('remembers an early dismissal across visits and reopens Help from the first tip', async () => {
    const { wrapper } = mountGuide()
    await flushPromises()
    await wrapper.get('[data-test="retry-filter-guide-next"]').trigger('click')
    await wrapper.get('[data-test="retry-filter-guide-dismiss"]').trigger('click')
    expect(localStorage.getItem(key(7))).toBe('seen')
    wrapper.unmount()
    const revisited = mountGuide().wrapper
    await flushPromises()
    expect(revisited.find('[role="dialog"]').exists()).toBe(false)
    await revisited.get('button').trigger('click')
    expect(revisited.get('[data-test="retry-filter-guide-step"]').attributes('data-step')).toBe('1')
  })

  it.each(['Escape', 'close button'])('remembers dismissal with the %s', async method => {
    const { wrapper } = mountGuide()
    await flushPromises()
    if (method === 'Escape') document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    else await wrapper.get('button[aria-label="Close modal"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(localStorage.getItem(key(7))).toBe('seen')
  })

  it('keeps dismissal separate for each administrator and reacts to account changes', async () => {
    localStorage.setItem(key(7), 'seen')
    const { wrapper, auth } = mountGuide()
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    auth.user = actor(8)
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    await wrapper.get('[data-test="retry-filter-guide-dismiss"]').trigger('click')
    expect(localStorage.getItem(key(8))).toBe('seen')
    auth.user = actor(7)
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('waits for the administrator identity before showing the first-visit guide', async () => {
    const { wrapper, auth } = mountGuide({ authenticated: false })
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    auth.user = actor(7)
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  })

  it('does not show admin help to ordinary users', async () => {
    const { wrapper } = mountGuide({ role: 'user' })
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(localStorage.getItem(key(7))).toBeNull()
  })

  it('allows reading and closing the guide when local storage is unavailable', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('Unavailable', 'SecurityError') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('Unavailable', 'SecurityError') })
    const { wrapper } = mountGuide()
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    await wrapper.get('[data-test="retry-filter-guide-dismiss"]').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  })

  it('renders all five tips and their controls in English', async () => {
    const { wrapper } = mountGuide({ locale: 'en' })
    await flushPromises()
    expect(wrapper.get('button').text()).toBe('Help')
    const titles = [en.retryFilter.guide.enableTitle, en.retryFilter.guide.ruleSetTitle, en.retryFilter.guide.bindingTitle, en.retryFilter.guide.retryTitle, en.retryFilter.guide.orderTitle]
    expect(wrapper.get('[data-test="retry-filter-guide-extra"] summary').text()).toBe('Extra notes for special cases')
    for (const title of titles) {
      expect(wrapper.get('[data-test="retry-filter-guide-step"] h4').text()).toBe(title)
      expect(wrapper.text()).not.toContain('admin.retryFilter')
      await wrapper.get('[data-test="retry-filter-guide-next"]').trigger('click')
    }
    expect(localStorage.getItem(key(7))).toBe('seen')
  })
})
