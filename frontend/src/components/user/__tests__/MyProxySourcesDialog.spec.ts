import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Pagination from '@/components/common/Pagination.vue'
import MyResourcesView from '@/views/user/MyResourcesView.vue'
import MyProxySourcesDialog from '../MyProxySourcesDialog.vue'

const api = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn(), sync: vi.fn(), syncAll: vi.fn(),
  listProxies: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn(), showError: vi.fn(),
}))

vi.mock('@/api/myResources', () => ({ myResourcesApi: { proxies: { sources: api, list: api.listProxies } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: api.showSuccess, showWarning: api.showWarning, showError: api.showError }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ meta: { resource: 'proxies' }, query: {} }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }),
}))

const TableStub = defineComponent({
  props: ['columns', 'data', 'loading'],
  template: `<div>
    <p v-if="loading" data-test="source-loading">Loading</p>
    <template v-else>
      <div v-for="row in data" :key="row.id" :data-test="'source-row-' + row.id">
        <div v-for="column in columns" :key="column.key"><slot :name="'cell-' + column.key" :row="row" /></div>
      </div>
      <slot v-if="!data.length" name="empty" />
    </template>
  </div>`,
})

const SelectStub = defineComponent({
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  template: '<select :value="modelValue"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
})

const source = {
  id: 7,
  name: 'Owned source',
  subscription_url: 'https://example.test/private-path?token=fixture-token',
  refresh_interval_minutes: 60,
  is_public: true,
  sync_enabled: true,
  node_count: 4,
  active_node_count: 3,
  last_sync_status: 'success',
}

function page(items = [source], total = items.length) {
  return { items, total, page: 1, page_size: 100, pages: Math.ceil(total / 100) }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((onResolve, onReject) => { resolve = onResolve; reject = onReject })
  return { promise, resolve, reject }
}

function mountDialog(show = true) {
  return mount(MyProxySourcesDialog, {
    props: { show },
    global: { stubs: { teleport: true, transition: false, DataTable: TableStub, Icon: true } },
  })
}

function mountResources() {
  return shallowMount(MyResourcesView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
        MyProxySourcesDialog: false,
        BaseDialog: false,
        Pagination: false,
        DataTable: TableStub,
        Select: SelectStub,
        Icon: true,
        teleport: true,
        transition: false,
      },
    },
  })
}

async function openManager(wrapper: ReturnType<typeof mountResources>) {
  await wrapper.get('button[title^="myResources.actions.sources"]').trigger('click')
  await flushPromises()
}

enableAutoUnmount(afterEach)

describe('MyProxySourcesDialog', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.listProxies.mockResolvedValue(page([]))
    api.list.mockResolvedValue(page())
    api.create.mockResolvedValue({ id: 8 })
    api.update.mockResolvedValue(source)
    api.delete.mockResolvedValue({})
    api.sync.mockResolvedValue({ imported_count: 4, errors: [] })
    api.syncAll.mockResolvedValue({ total: 1, success_count: 1, created_count: 2, updated_count: 1 })
  })

  it('loads only when opened, masks source URLs and filters by the selected owner source', async () => {
    const wrapper = mountDialog(false)
    expect(api.list).not.toHaveBeenCalled()
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(api.list).toHaveBeenCalledWith({ page: 1, page_size: 100 })
    expect(wrapper.emitted('loaded')?.[0]).toEqual([page()])
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.html()).toContain('https://example.test/***')
    expect(wrapper.html()).not.toContain('fixture-token')
    expect(wrapper.html()).not.toContain('private-path')
    expect(wrapper.get('.badge-success').text()).toContain('myResources.states.public')
    expect(wrapper.text()).toContain('admin.proxies.sourceStatusSuccess')
    await wrapper.get('[data-test="user-proxy-source-filter-7"]').trigger('click')
    expect(wrapper.emitted('filter')?.[0]).toEqual([source])
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('keeps a failed create open, preserves input and prevents duplicate saves', async () => {
    const saving = deferred<unknown>()
    api.create.mockReturnValueOnce(saving.promise)
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="user-proxy-source-add"]').trigger('click')
    await wrapper.get('#user-proxy-source-name').setValue('New source')
    await wrapper.get('#user-proxy-source-url').setValue(source.subscription_url)
    await wrapper.get('[data-test="user-proxy-source-is-public"]').setValue(true)
    await wrapper.get('[data-test="user-proxy-source-sync-enabled"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    expect(api.create).toHaveBeenCalledTimes(1)
    expect(api.create).toHaveBeenCalledWith({
      name: 'New source', subscription_url: source.subscription_url,
      refresh_interval_minutes: 1440, is_public: true, sync_enabled: false,
    })
    expect(wrapper.get('[data-test="user-proxy-source-editor-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="user-proxy-source-editor-cancel"]').attributes('disabled')).toBeDefined()
    saving.reject({ message: 'Save unavailable' })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Save unavailable')
    expect((wrapper.get('#user-proxy-source-name').element as HTMLInputElement).value).toBe('New source')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.create).toHaveBeenCalledTimes(2)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('edits an existing source and validates its interval before updating', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="user-proxy-source-edit-7"]').trigger('click')
    expect((wrapper.get('#user-proxy-source-url').element as HTMLInputElement).value).toBe(source.subscription_url)
    await wrapper.get('#user-proxy-source-interval').setValue(4)
    await wrapper.get('form').trigger('submit')
    expect(api.update).not.toHaveBeenCalled()
    await wrapper.get('#user-proxy-source-interval').setValue(120)
    await wrapper.get('[data-test="user-proxy-source-is-public"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenCalledWith(7, {
      name: source.name, subscription_url: source.subscription_url,
      refresh_interval_minutes: 120, is_public: false, sync_enabled: true,
    })
    expect(api.create).not.toHaveBeenCalled()
  })

  it('preserves source fields while pausing and blocks overlapping source mutations', async () => {
    const updating = deferred<unknown>()
    api.update.mockReturnValueOnce(updating.promise)
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="user-proxy-source-toggle-7"]').trigger('click')
    await wrapper.get('[data-test="user-proxy-source-toggle-7"]').trigger('click')
    await wrapper.get('[data-test="user-proxy-source-sync-all"]').trigger('click')
    expect(api.update).toHaveBeenCalledTimes(1)
    expect(api.syncAll).not.toHaveBeenCalled()
    expect(api.update).toHaveBeenCalledWith(7, {
      name: source.name, subscription_url: source.subscription_url,
      refresh_interval_minutes: 60, is_public: true, sync_enabled: false,
    })
    api.list.mockResolvedValue(page([{ ...source, sync_enabled: false }]))
    updating.resolve({})
    await flushPromises()
    expect(wrapper.get('[data-test="user-proxy-source-toggle-7"]').attributes('aria-label')).toContain('actions.resumeSync')
  })

  it('reports partial single-source syncs and exposes sync-all counts without raw result details', async () => {
    api.sync.mockResolvedValue({ created_count: 2, updated_count: 1, errors: ['fixture-node-error'] })
    api.syncAll.mockResolvedValue({
      total: 2, failed_count: 1, partial_count: 1, created_count: 2,
      sources: [{ subscription_url: source.subscription_url, error: 'fixture-node-error' }],
    })
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="user-proxy-source-sync-7"]').trigger('click')
    await flushPromises()
    expect(api.sync).toHaveBeenCalledWith(7)
    expect(api.showWarning).toHaveBeenCalledWith('admin.proxies.sourceSyncPartial:{"count":3,"errors":1}')
    await wrapper.get('[data-test="user-proxy-source-sync-all"]').trigger('click')
    await flushPromises()
    expect(api.syncAll).toHaveBeenCalledTimes(1)
    const summary = wrapper.get('[data-test="user-proxy-source-sync-all-summary"]').text()
    expect(summary).toContain('"failed":1')
    expect(summary).toContain('"partial":1')
    expect(summary).not.toContain('fixture-token')
    expect(summary).not.toContain('fixture-node-error')
    expect(wrapper.emitted('changed')).toHaveLength(2)
  })

  it('keeps sync errors in the manager and allows a retry', async () => {
    api.sync.mockRejectedValueOnce({ message: 'Sync unavailable' })
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="user-proxy-source-sync-7"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Sync unavailable')
    expect(wrapper.get('[data-test="user-proxy-source-sync-7"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.emitted('deleted')).toBeUndefined()
    await wrapper.get('[data-test="user-proxy-source-sync-7"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('requires delete confirmation, prevents repeat deletes and returns from an emptied last page', async () => {
    const lastSource = { ...source, id: 107 }
    const deleting = deferred<unknown>()
    api.list.mockResolvedValueOnce(page([source], 101)).mockResolvedValueOnce(page([lastSource], 101)).mockResolvedValue(page([source], 100))
    api.delete.mockReturnValueOnce(deleting.promise)
    const wrapper = mountDialog()
    await flushPromises()
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    expect(api.list).toHaveBeenLastCalledWith({ page: 2, page_size: 100 })
    await wrapper.get('[data-test="user-proxy-source-delete-107"]').trigger('click')
    expect(api.delete).not.toHaveBeenCalled()
    await wrapper.get('[data-test="user-proxy-source-delete-cancel"]').trigger('click')
    expect(api.delete).not.toHaveBeenCalled()
    await wrapper.get('[data-test="user-proxy-source-delete-107"]').trigger('click')
    await wrapper.get('[data-test="user-proxy-source-delete-confirm"]').trigger('click')
    await wrapper.get('[data-test="user-proxy-source-delete-confirm"]').trigger('click')
    expect(api.delete).toHaveBeenCalledTimes(1)
    expect(api.delete).toHaveBeenCalledWith(107)
    expect(wrapper.get('[data-test="user-proxy-source-delete-cancel"]').attributes('disabled')).toBeDefined()
    deleting.resolve({})
    await flushPromises()
    expect(api.list).toHaveBeenLastCalledWith({ page: 1, page_size: 100 })
    expect(wrapper.findComponent(Pagination).exists()).toBe(false)
    expect(wrapper.find('[data-test="source-row-107"]').exists()).toBe(false)
    expect(wrapper.emitted('deleted')).toEqual([[107]])
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('shows delete failures inside the confirmation and retains the source for retry', async () => {
    api.delete.mockRejectedValueOnce({ message: 'Delete unavailable' })
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="user-proxy-source-delete-7"]').trigger('click')
    await wrapper.get('[data-test="user-proxy-source-delete-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Delete unavailable')
    expect(wrapper.find('[data-test="source-row-7"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="user-proxy-source-delete-confirm"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(wrapper.emitted('deleted')).toBeUndefined()
  })

  it('clamps a stale page after the server total shrinks', async () => {
    api.list.mockResolvedValueOnce(page([source], 101)).mockResolvedValueOnce(page([], 100)).mockResolvedValueOnce(page([source], 100))
    const wrapper = mountDialog()
    await flushPromises()
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    expect(api.list.mock.calls.map(([params]) => params.page)).toEqual([1, 2, 1])
    expect(wrapper.find('[data-test="source-row-7"]').exists()).toBe(true)
  })

  it('shows loading failures and ignores a response from a previous opening', async () => {
    const oldRequest = deferred<ReturnType<typeof page>>()
    api.list.mockRejectedValueOnce({ message: 'Load unavailable' }).mockReturnValueOnce(oldRequest.promise).mockResolvedValueOnce(page([{ ...source, name: 'Current source' }]))
    const wrapper = mountDialog()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Load unavailable')
    await wrapper.get('[data-test="user-proxy-source-refresh"]').trigger('click')
    expect(wrapper.find('[data-test="source-loading"]').exists()).toBe(true)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    oldRequest.resolve(page([{ ...source, name: 'Stale source' }]))
    await flushPromises()
    expect(wrapper.text()).toContain('Current source')
    expect(wrapper.text()).not.toContain('Stale source')
  })

  it('recovers the parent source filter when a manager load succeeds after preload failure', async () => {
    api.list.mockRejectedValueOnce({ message: 'Preload unavailable' }).mockResolvedValue(page())
    const wrapper = mountResources()
    await flushPromises()
    expect(wrapper.find('[data-test="my-proxy-source-filter"]').exists()).toBe(false)
    await openManager(wrapper)
    await wrapper.get('[data-test="user-proxy-source-close"]').trigger('click')
    expect(wrapper.get('[data-test="my-proxy-source-filter"]').text()).toContain(source.name)
    expect(api.list).toHaveBeenCalledTimes(2)
  })

  it('updates parent filter options on manual refresh without issuing another list request', async () => {
    const removed = { ...source, id: 8, name: 'Removed source' }
    const added = { ...source, id: 9, name: 'Added source' }
    api.list.mockResolvedValue(page([source, removed]))
    const wrapper = mountResources()
    await flushPromises()
    await openManager(wrapper)
    api.list.mockResolvedValue(page([{ ...source, name: 'Renamed source' }, added]))
    await wrapper.get('[data-test="user-proxy-source-refresh"]').trigger('click')
    await flushPromises()
    const options = wrapper.get('[data-test="my-proxy-source-filter"]').text()
    expect(options).toContain('Renamed source')
    expect(options).toContain('Added source')
    expect(options).not.toContain('Removed source')
    expect(api.list).toHaveBeenCalledTimes(3)
  })

  it('preserves a selection from a later page, then removes it and clears the proxy filter on delete', async () => {
    const laterSource = { ...source, id: 107, name: 'Later source' }
    api.list.mockResolvedValueOnce(page([source], 101))
      .mockResolvedValueOnce(page([source], 101))
      .mockResolvedValueOnce(page([laterSource], 101))
      .mockResolvedValueOnce(page([source], 101))
      .mockResolvedValueOnce(page([laterSource], 101))
      .mockResolvedValue(page([source], 100))
    const wrapper = mountResources()
    await flushPromises()
    await openManager(wrapper)
    wrapper.getComponent(MyProxySourcesDialog).getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    await wrapper.get('[data-test="user-proxy-source-filter-107"]').trigger('click')
    await flushPromises()
    await openManager(wrapper)
    expect(wrapper.get('[data-test="my-proxy-source-filter"]').text()).toContain('Later source')
    expect((wrapper.get('[data-test="my-proxy-source-filter"]').element as HTMLSelectElement).value).toBe('107')
    wrapper.getComponent(MyProxySourcesDialog).getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    await wrapper.get('[data-test="user-proxy-source-delete-107"]').trigger('click')
    await wrapper.get('[data-test="user-proxy-source-delete-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="my-proxy-source-filter"]').text()).not.toContain('Later source')
    expect((wrapper.get('[data-test="my-proxy-source-filter"]').element as HTMLSelectElement).value).toBe('')
    expect(api.listProxies).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, source_id: undefined }))
    expect(api.list).toHaveBeenCalledTimes(6)
  })

  it('clears an externally deleted selection when a complete refresh no longer includes it', async () => {
    const remaining = { ...source, id: 8, name: 'Remaining source' }
    api.list.mockResolvedValue(page([source, remaining]))
    const wrapper = mountResources()
    await flushPromises()
    await openManager(wrapper)
    await wrapper.get('[data-test="user-proxy-source-filter-7"]').trigger('click')
    await flushPromises()
    await openManager(wrapper)
    api.list.mockResolvedValue(page([remaining]))
    await wrapper.get('[data-test="user-proxy-source-refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="my-proxy-source-filter"]').text()).not.toContain(source.name)
    expect((wrapper.get('[data-test="my-proxy-source-filter"]').element as HTMLSelectElement).value).toBe('')
    expect(api.listProxies).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, source_id: undefined }))
  })

  it('does not let an older preload overwrite the manager refresh', async () => {
    const preload = deferred<ReturnType<typeof page>>()
    api.list.mockReturnValueOnce(preload.promise).mockResolvedValue(page([{ ...source, name: 'Fresh source' }]))
    const wrapper = mountResources()
    await openManager(wrapper)
    preload.resolve(page([{ ...source, name: 'Stale source' }]))
    await flushPromises()
    expect(wrapper.get('[data-test="my-proxy-source-filter"]').text()).toContain('Fresh source')
    expect(wrapper.get('[data-test="my-proxy-source-filter"]').text()).not.toContain('Stale source')
    expect(api.list).toHaveBeenCalledTimes(2)
  })
})
