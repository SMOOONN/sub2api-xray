import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { RetryFilterMonitorResponse } from '@/api/admin/retryFilter'
import DataTable from '@/components/common/DataTable.vue'
import RetryFilterMonitorView from '../RetryFilterMonitorView.vue'

const { listMonitor } = vi.hoisted(() => ({ listMonitor: vi.fn() }))

vi.mock('@/api/admin/retryFilter', () => ({ default: { listMonitor } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

enableAutoUnmount(afterEach)
const initialAppConfig = window.__APP_CONFIG__

function mountMonitor() {
  return mount(RetryFilterMonitorView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        RouterLink: { template: '<a><slot /></a>' },
        DataTable: true,
        BaseDialog: true,
        Select: true,
        Toggle: true,
        Icon: true
      }
    }
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function monitorResponse(page: number, id: string, total = 90): RetryFilterMonitorResponse {
  return {
    items: [{ id, created_at: '2026-10-12T00:00:00Z', action: 'observe' }],
    page,
    page_size: 30,
    total,
    summary: { matched: total, retries: 0, terminates: 0 }
  }
}

describe('retry filter monitor pagination', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    listMonitor.mockReset().mockImplementation(async (query: { page: number; page_size: number }) => ({
      items: [{ id: '1:1', created_at: '2026-10-12T00:00:00Z', action: 'observe' }],
      total: 90,
      page: query.page,
      page_size: Math.min(query.page_size, 100),
      pages: Math.ceil(90 / Math.min(query.page_size, 100)),
      summary: { matched: 90, retries: 0, terminates: 0, by_action: {} }
    }))
  })

  afterEach(() => {
    window.__APP_CONFIG__ = initialAppConfig
    vi.useRealTimers()
  })

  it.each([
    { name: 'standard', options: [10, 20, 50, 100], defaultSize: 20 },
    { name: 'larger than the monitor API limit', options: [500, 1000], defaultSize: 500 }
  ])('preserves 30 rows per page with $name global options', async ({ options, defaultSize }) => {
    window.__APP_CONFIG__ = {
      table_default_page_size: defaultSize,
      table_page_size_options: options
    } as Window['__APP_CONFIG__']
    const wrapper = mountMonitor()
    await flushPromises()

    expect(listMonitor).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, page_size: 30 }))
    expect(wrapper.find('.page-size-select').exists()).toBe(false)

    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    await flushPromises()
    expect(listMonitor).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 30 }))
  })

  it.each(['success', 'failure'])('keeps reset results when an older page request ends with %s', async (outcome) => {
    const wrapper = mountMonitor()
    await flushPromises()
    const older = deferred<RetryFilterMonitorResponse>()
    const newer = deferred<RetryFilterMonitorResponse>()
    listMonitor.mockImplementationOnce(() => older.promise).mockImplementationOnce(() => newer.promise)

    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    expect(listMonitor).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))
    await wrapper.findAll('button').find(button => button.text() === 'common.reset')!.trigger('click')
    expect(listMonitor).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1 }))
    const resetResult = monitorResponse(1, 'reset:1', 45)
    newer.resolve(resetResult)
    await flushPromises()

    if (outcome === 'success') older.resolve(monitorResponse(2, 'stale:1'))
    else older.reject({ response: { data: { message: 'stale failure' } } })
    await flushPromises()

    expect(wrapper.get('button[aria-current="page"]').text()).toBe('1')
    expect(wrapper.getComponent(DataTable).props()).toMatchObject({ data: resetResult.items, loading: false })
    expect(wrapper.get('section[aria-busy]').attributes('aria-busy')).toBe('false')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.getComponent({ name: 'Pagination' }).props('total')).toBe(45)
    expect(wrapper.findAll('.grid > .card')[0].text()).toContain('45')

    await vi.advanceTimersByTimeAsync(15000)
    await flushPromises()
    expect(listMonitor).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, page_size: 30 }))
  })

  it.each(['success', 'failure'])('keeps reset loading while an older page request ends with %s', async (outcome) => {
    const wrapper = mountMonitor()
    await flushPromises()
    const initialItems = wrapper.getComponent(DataTable).props('data')
    const older = deferred<RetryFilterMonitorResponse>()
    const newer = deferred<RetryFilterMonitorResponse>()
    listMonitor.mockImplementationOnce(() => older.promise).mockImplementationOnce(() => newer.promise)

    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'common.reset')!.trigger('click')
    if (outcome === 'success') older.resolve(monitorResponse(2, 'stale:1'))
    else older.reject({ response: { data: { message: 'stale failure' } } })
    await flushPromises()

    expect(wrapper.get('button[aria-current="page"]').text()).toBe('1')
    expect(wrapper.getComponent(DataTable).props()).toMatchObject({ data: initialItems, loading: true })
    expect(wrapper.get('section[aria-busy]').attributes('aria-busy')).toBe('true')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.findAll('button').find(button => button.text() === 'common.search')!.attributes('disabled')).toBeDefined()

    const resetResult = monitorResponse(1, 'reset:1', 45)
    newer.resolve(resetResult)
    await flushPromises()
    expect(wrapper.getComponent(DataTable).props()).toMatchObject({ data: resetResult.items, loading: false })
    expect(wrapper.get('button[aria-current="page"]').text()).toBe('1')
  })

  it('shows the current request failure and recovers on refresh', async () => {
    listMonitor.mockRejectedValueOnce({ response: { data: { message: 'current failure' } } })
    const wrapper = mountMonitor()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('current failure')
    expect(wrapper.get('section[aria-busy]').attributes('aria-busy')).toBe('false')

    const refreshed = monitorResponse(1, 'refreshed:1')
    listMonitor.mockResolvedValueOnce(refreshed)
    await wrapper.findAll('button').find(button => button.text() === 'common.refresh')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.getComponent(DataTable).props()).toMatchObject({ data: refreshed.items, loading: false })
  })
})
