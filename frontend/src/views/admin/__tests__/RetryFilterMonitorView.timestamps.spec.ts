import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { RetryFilterMonitorEvent } from '@/api/admin/retryFilter'
import RetryFilterMonitorView from '../RetryFilterMonitorView.vue'

const { listMonitor, getMonitorEvent } = vi.hoisted(() => ({ listMonitor: vi.fn(), getMonitorEvent: vi.fn() }))

vi.mock('@/api/admin/retryFilter', () => ({ default: { listMonitor, getMonitorEvent } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

enableAutoUnmount(afterEach)

describe('retry monitor attempt timestamps', () => {
  beforeEach(() => {
    listMonitor.mockReset()
    getMonitorEvent.mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it.each([{ width: 1280, name: 'desktop' }, { width: 390, name: 'narrow' }])('shows milliseconds in $name records and detail', async ({ width }) => {
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
      matches: width >= 768, media: query, onchange: null,
      addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(),
      removeEventListener: vi.fn(), dispatchEvent: vi.fn()
    }))
    const events: RetryFilterMonitorEvent[] = [
      { id: '481:2', created_at: '2026-10-12T11:09:26.992Z', action: 'same_account_retry' },
      { id: '481:1', created_at: '2026-10-12T11:09:26.842Z', action: 'same_account_retry' }
    ]
    listMonitor.mockResolvedValue({ items: events, total: 2, page: 1, page_size: 30 })
    getMonitorEvent.mockResolvedValue(events[0])
    const wrapper = mount(RetryFilterMonitorView, {
      attachTo: document.body,
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          RouterLink: { template: '<a><slot /></a>' },
          Select: true, Toggle: true, Icon: true
        }
      }
    })
    await flushPromises()

    const times = wrapper.findAll('time')
    expect(times).toHaveLength(2)
    expect(times.map(time => time.attributes('datetime'))).toEqual(events.map(event => event.created_at))
    expect(times[0].text()).toMatch(/\.992$/)
    expect(times[1].text()).toMatch(/\.842$/)
    expect(times[0].text()).not.toBe(times[1].text())
    if (width >= 768) expect(wrapper.find('table').exists()).toBe(true)
    else expect(wrapper.findAll('[data-field="created_at"]')).toHaveLength(2)

    await wrapper.findAll('button').find(button => button.text() === 'common.view')!.trigger('click')
    await flushPromises()
    expect(getMonitorEvent).toHaveBeenCalledWith('481:2')
    const detailTime = document.querySelector('[role="dialog"] time')
    expect(detailTime?.getAttribute('datetime')).toBe(events[0].created_at)
    expect(detailTime?.textContent).toBe(times[0].text())
  })
})
