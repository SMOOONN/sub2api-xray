import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'

const {
  adminAccountSyncMock,
  adminPreviewSyncMock,
  userAccountSyncMock,
  userPreviewSyncMock,
  copyToClipboard,
  showError,
  showSuccess,
  showInfo,
  showWarning
} = vi.hoisted(() => ({
  adminAccountSyncMock: vi.fn(),
  adminPreviewSyncMock: vi.fn(),
  userAccountSyncMock: vi.fn(),
  userPreviewSyncMock: vi.fn(),
  copyToClipboard: vi.fn().mockResolvedValue(true),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  showWarning: vi.fn()
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    syncUpstreamModels: adminAccountSyncMock,
    syncUpstreamModelsPreview: adminPreviewSyncMock
  },
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('@/api/myResources', () => ({
  myResourcesApi: {
    accounts: {
      syncUpstreamModels: userAccountSyncMock,
      syncUpstreamModelsPreview: userPreviewSyncMock,
      getAntigravityDefaultModelMapping: vi.fn()
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo,
    showWarning
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => key === 'common.copy' ? 'Copy' : key === 'admin.accounts.modelMappingConflict' ? `Model mapping conflict: ${params?.from} → ${params?.to}` : key === 'admin.accounts.syncUpstreamModelsError' ? `${key}: ${params?.message}` : key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

const globalOptions = {
  stubs: {
    ModelIcon: true,
    Icon: true
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

function mountSelector(props: Record<string, unknown> = {}) {
  return mount(ModelWhitelistSelector, {
    props: {
      modelValue: [],
      platform: 'openai',
      ...props,
    },
    global: globalOptions
  })
}

function syncButton(wrapper: ReturnType<typeof mountSelector>) {
  return wrapper.get('[data-testid="sync-upstream-models"]')
}

function findModelRow(wrapper: ReturnType<typeof mountSelector>, modelId: string) {
  const row = wrapper
    .findAll('[data-testid="model-option"]')
    .find(candidate => candidate.text().includes(modelId))

  if (!row) {
    throw new Error('Model row not found: ' + modelId)
  }

  return row
}

describe('ModelWhitelistSelector', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    copyToClipboard.mockResolvedValue(true)
    adminAccountSyncMock.mockResolvedValue({ models: ['admin-model'] })
    adminPreviewSyncMock.mockResolvedValue({ models: ['admin-preview'] })
    userAccountSyncMock.mockResolvedValue({ models: ['user-model'] })
    userPreviewSyncMock.mockResolvedValue({ models: ['user-preview'] })
    showError.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
  })

  it('keeps the existing admin account sync as the default', async () => {
    const wrapper = mount(ModelWhitelistSelector, {
      props: { modelValue: [], platform: 'openai', accountId: 7 },
      global: globalOptions
    })

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    expect(adminAccountSyncMock).toHaveBeenCalledWith(7)
    expect(userAccountSyncMock).not.toHaveBeenCalled()
  })

  it('keeps the existing admin preview sync as the default', async () => {
    const credentials = {
      platform: 'openai',
      type: 'apikey',
      base_url: 'https://api.example.com',
      api_key: 'temporary-key'
    }
    const wrapper = mount(ModelWhitelistSelector, {
      props: { modelValue: [], platform: 'openai', syncCredentials: credentials },
      global: globalOptions
    })

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    expect(adminPreviewSyncMock).toHaveBeenCalledWith(credentials)
    expect(userPreviewSyncMock).not.toHaveBeenCalled()
  })

  it.each(['openai', 'command_code', 'cline'])('uses only the user account sync API for %s in user scope', async (platform) => {
    const wrapper = mount(ModelWhitelistSelector, {
      props: { modelValue: [], platform, accountId: 8, scope: 'user' },
      global: globalOptions
    })

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    expect(userAccountSyncMock).toHaveBeenCalledWith(8)
    expect(adminAccountSyncMock).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual(['user-model'])
  })

  it.each(['openai', 'command_code', 'cline'])('uses only the user preview API for %s temporary credentials', async (platform) => {
    const credentials = {
      platform,
      type: 'apikey',
      base_url: 'https://api.example.com',
      api_key: 'temporary-key'
    }
    const wrapper = mount(ModelWhitelistSelector, {
      props: { modelValue: [], platform, syncCredentials: credentials, scope: 'user' },
      global: globalOptions
    })

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    expect(userPreviewSyncMock).toHaveBeenCalledWith(credentials)
    expect(adminPreviewSyncMock).not.toHaveBeenCalled()
  })

  it('prefers an injected account callback over built-in APIs', async () => {
    const callback = vi.fn().mockResolvedValue({ models: ['callback-model'] })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 9,
        scope: 'user',
        syncAccountModels: callback
      },
      global: globalOptions
    })

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    expect(callback).toHaveBeenCalledWith(9)
    expect(userAccountSyncMock).not.toHaveBeenCalled()
    expect(adminAccountSyncMock).not.toHaveBeenCalled()
  })

  it('prefers an injected preview callback over built-in APIs', async () => {
    const credentials = {
      platform: 'openai',
      type: 'apikey',
      base_url: 'https://api.example.com',
      api_key: 'temporary-key'
    }
    const callback = vi.fn().mockResolvedValue({ models: ['callback-preview'] })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        scope: 'user',
        syncCredentials: credentials,
        syncPreviewModels: callback
      },
      global: globalOptions
    })

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    expect(callback).toHaveBeenCalledWith(credentials)
    expect(userPreviewSyncMock).not.toHaveBeenCalled()
    expect(adminPreviewSyncMock).not.toHaveBeenCalled()
  })

  afterEach(() => {
    resetPlatformCatalog()
  })

  it.each(['anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'command_code', 'cline'])(
    'supports upstream sync for %s saved accounts and creation previews',
    (platform) => {
      const wrappers = ['admin', 'user'].flatMap(scope => [
        mountSelector({ platform, accountId: 46, scope }),
        mountSelector({ platform, syncCredentials: { platform, type: 'apikey', api_key: 'test-key' }, scope })
      ])
      for (const wrapper of wrappers) {
        expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(true)
        wrapper.unmount()
      }
    }
  )

  it.each(['typesafe', 'unregistered'])(
    'hides upstream sync for unsupported %s saved accounts and creation previews',
    (platform) => {
      const wrappers = [
        mountSelector({ platform, accountId: 46 }),
        mountSelector({ platform, syncCredentials: { platform, type: 'apikey', api_key: 'test-key' } })
      ]
      for (const wrapper of wrappers) {
        expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(false)
        wrapper.unmount()
      }
      expect(adminAccountSyncMock).not.toHaveBeenCalled()
      expect(adminPreviewSyncMock).not.toHaveBeenCalled()
      expect(userAccountSyncMock).not.toHaveBeenCalled()
      expect(userPreviewSyncMock).not.toHaveBeenCalled()
    }
  )

  it('requires a supported request builder for newly registered platforms', async () => {
    const wrapper = mountSelector({ platform: 'acme_router', accountId: 46 })
    const platforms = [
      ...BUILTIN_PLATFORM_CATALOG.platforms,
      { id: 'acme_router', display_name: 'Acme Router', gateway: 'openai' as const, cn_provider: false }
    ]
    setPlatformCatalog({ ...BUILTIN_PLATFORM_CATALOG, platforms })
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(false)

    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: platforms.map(spec => spec.id === 'acme_router'
        ? { ...spec, multi_protocol: { default_mode: 'default', routing: 'by_inbound', modes: [] } }
        : spec)
    })
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(true)
    wrapper.unmount()
  })

  it('rejects a custom whitelist model that is already mapped to a different target', async () => {
    const wrapper = mountSelector({ modelMappings: [{ from: 'gpt-latest', to: 'deepseek-chat' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue(' gpt-latest ')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showInfo).toHaveBeenCalledWith(expect.stringContaining('gpt-latest → deepseek-chat'))
  })

  it('keeps the existing duplicate identity warning before checking mappings', async () => {
    const wrapper = mountSelector({ modelValue: ['gpt-latest'], modelMappings: [{ from: 'gpt-latest', to: 'deepseek-chat' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('gpt-latest')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(showInfo).toHaveBeenCalledWith('admin.accounts.modelExists')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('allows matching identity mapping as a whitelist model', async () => {
    const wrapper = mountSelector({ modelMappings: [{ from: 'gpt-latest', to: 'gpt-latest' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('gpt-latest')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-latest']]])
  })

  it('still allows custom models without a mapping prop', async () => {
    const wrapper = mountSelector()
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('custom-model')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['custom-model']]])
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('Copy gpt-5.6-sol')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-5.6-sol')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps the existing model selection behavior', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('warns when model IDs sync but capability metadata is incomplete', async () => {
    adminAccountSyncMock.mockResolvedValue({
      models: ['x-preview-f-free'],
      warnings: [
        {
          code: 'upstream_model_metadata_incomplete',
          message: 'Model IDs were synced, but capability metadata could not be updated.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataIncomplete')
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('shows the backend message when upstream sync is rejected', async () => {
    adminAccountSyncMock.mockRejectedValue({
      status: 400,
      code: 'BAD_REQUEST',
      message: 'Vertex API key accounts do not support upstream model listing'
    })
    const wrapper = mountSelector({ platform: 'gemini', accountId: 174 })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(
      'admin.accounts.syncUpstreamModelsError: Vertex API key accounts do not support upstream model listing'
    )
  })

  it('shows success and a partial warning when some capabilities were saved', async () => {
    adminAccountSyncMock.mockResolvedValue({
      models: ['gpt-6-astra', 'gpt-image-2'],
      warnings: [
        {
          code: 'upstream_model_metadata_partial',
          message: 'Some model capabilities were saved; remaining models are still incomplete.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-6-astra', 'gpt-image-2']]])
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsSuccess')
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataPartial')
  })

  it('reports a successful preview so account creation can persist metadata', async () => {
    adminPreviewSyncMock.mockResolvedValue({
      models: ['x-preview-f-free'],
      metadata: {
        'x-preview-f-free': {
          id: 'x-preview-f-free',
          reasoning: true,
          supported_reasoning_levels: ['low', 'high', 'max'],
        },
      },
    })
    const wrapper = mountSelector({
      syncCredentials: {
        platform: 'openai',
        type: 'apikey',
        base_url: 'https://opencode.ai/zen/v1',
        api_key: 'test-key',
      },
    })
    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

    expect(syncButton).toBeDefined()
    await syncButton?.trigger('click')
    await flushPromises()

    expect(adminPreviewSyncMock).toHaveBeenCalledOnce()
    expect(wrapper.emitted('upstream-synced')).toEqual([[]])
    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
  })

  it('does not append configured fallback IDs', async () => {
    adminAccountSyncMock.mockResolvedValue({
      models: ['composer-2.5'],
      discovery_source: 'configured_fallback',
      warnings: [{ code: 'grok_model_catalog_configured_fallback', message: 'fallback' }]
    })
    const wrapper = mountSelector({ platform: 'grok', accountId: 46, modelValue: ['grok-4.7'] })
    const button = wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.grokModelDiagnostics.configuredFallback')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('upstream-sync-finished')).toHaveLength(1)
  })

  it('still appends a real catalog when capability metadata is incomplete', async () => {
    adminAccountSyncMock.mockResolvedValue({
      models: ['grok-4.7'],
      discovery_source: 'upstream',
      warnings: [{ code: 'upstream_model_metadata_incomplete', message: 'incomplete' }]
    })
    const wrapper = mountSelector({ platform: 'grok', accountId: 46, modelValue: ['grok-4'] })
    const button = wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['grok-4', 'grok-4.7']]])
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataIncomplete')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('upstream-sync-finished')).toHaveLength(1)
    expect(wrapper.emitted('upstream-synced')).toBeUndefined()
  })

  it('keeps appending models when a non-grok sync omits discovery fields', async () => {
    adminAccountSyncMock.mockResolvedValue({ models: ['gpt-6'] })
    const wrapper = mountSelector({ platform: 'openai', accountId: 46, modelValue: [] })
    const button = wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-6']]])
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsSuccess')
    expect(wrapper.emitted('upstream-sync-finished')).toHaveLength(1)
    expect(wrapper.emitted('upstream-synced')).toBeUndefined()
  })

  it('keeps creation preview on the original upstream-synced event only', async () => {
    adminPreviewSyncMock.mockResolvedValue({
      models: ['grok-4.7'],
      discovery_source: 'upstream'
    })
    const wrapper = mountSelector({
      syncCredentials: {
        platform: 'grok',
        type: 'apikey',
        base_url: 'https://api.x.ai/v1',
        api_key: 'xai-test'
      }
    })
    const button = wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!
    await button.trigger('click')
    await flushPromises()
    expect(adminAccountSyncMock).not.toHaveBeenCalled()
    expect(wrapper.emitted('upstream-synced')).toEqual([[]])
    expect(wrapper.emitted('upstream-sync-finished')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['grok-4.7']]])
  })

  it('does not mark a creation preview synced when the catalog is only a configured fallback', async () => {
    adminPreviewSyncMock.mockResolvedValue({
      models: ['composer-2.5'],
      discovery_source: 'configured_fallback',
      warnings: [{ code: 'grok_model_catalog_configured_fallback', message: 'fallback' }]
    })
    const wrapper = mountSelector({
      syncCredentials: {
        platform: 'grok',
        type: 'apikey',
        api_key: 'xai-test'
      }
    })
    const button = wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('upstream-synced')).toBeUndefined()
    expect(wrapper.emitted('upstream-sync-finished')).toBeUndefined()
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.grokModelDiagnostics.configuredFallback')
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('emits completion when a saved account sync fails so the last attempt can refresh', async () => {
    adminAccountSyncMock.mockRejectedValue({
      status: 502,
      message: 'upstream unavailable'
    })
    const wrapper = mountSelector({ platform: 'grok', accountId: 46, modelValue: ['grok-4.7'] })
    const button = wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showError).toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('upstream-sync-finished')).toHaveLength(1)
  })

  it('shows the upstream sync button for OpenCode Go create-account credentials', () => {
    const wrapper = mountSelector({
      platform: 'opencode_go',
      syncCredentials: {
        platform: 'opencode_go',
        type: 'apikey',
        base_url: 'https://opencode.ai/zen/go/v1',
        api_key: 'sk-test',
      },
    })
    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

    expect(syncButton).toBeDefined()
    expect(syncButton?.exists()).toBe(true)
  })

  it('can hide only the built-in trigger while retaining its public sync action', async () => {
    adminAccountSyncMock.mockResolvedValue({ models: ['grok-4.7'], discovery_source: 'upstream' })
    const wrapper = mountSelector({ platform: 'grok', accountId: 46, showSyncButton: false })
    expect(wrapper.find('[data-testid="sync-upstream-models"]').exists()).toBe(false)
    await wrapper.vm.syncUpstreamModels()
    await flushPromises()
    expect(adminAccountSyncMock).toHaveBeenCalledTimes(1)
    expect(adminAccountSyncMock).toHaveBeenCalledWith(46)
    expect(wrapper.emitted('update:modelValue')).toEqual([[['grok-4.7']]])
    expect(wrapper.emitted('upstream-sync-finished')).toEqual([[]])
    wrapper.unmount()
  })

  it('keeps the public busy flag on the newer sync when an older account sync settles', async () => {
    const first = deferred<any>()
    const second = deferred<any>()
    adminAccountSyncMock.mockImplementation((id: number) => (id === 1 ? first.promise : second.promise))
    const wrapper = mountSelector({ platform: 'grok', accountId: 1, modelValue: ['a-local'], showSyncButton: false })

    const firstSync = wrapper.vm.syncUpstreamModels()
    expect(wrapper.vm.isSyncingUpstream).toBe(true)

    await wrapper.setProps({ accountId: 2, modelValue: ['b-local'] })
    expect(wrapper.vm.isSyncingUpstream).toBe(false)
    const secondSync = wrapper.vm.syncUpstreamModels()
    expect(wrapper.vm.isSyncingUpstream).toBe(true)

    first.resolve({ models: ['a-private-catalog'], discovery_source: 'upstream' })
    await firstSync
    await flushPromises()
    expect(wrapper.vm.isSyncingUpstream).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('upstream-sync-finished')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()

    second.resolve({ models: ['b-catalog'], discovery_source: 'upstream' })
    await secondSync
    await flushPromises()
    expect(wrapper.vm.isSyncingUpstream).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toEqual([[['b-local', 'b-catalog']]])
    expect(wrapper.emitted('upstream-sync-finished')).toEqual([[]])
    expect(showSuccess).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('still withholds configured fallback ids when the built-in trigger is hidden', async () => {
    adminAccountSyncMock.mockResolvedValue({
      models: ['composer-2.5'],
      discovery_source: 'configured_fallback',
      warnings: [{ code: 'grok_model_catalog_configured_fallback', message: 'fallback' }]
    })
    const wrapper = mountSelector({ platform: 'grok', accountId: 46, modelValue: ['grok-4.7'], showSyncButton: false })
    await wrapper.vm.syncUpstreamModels()
    await flushPromises()
    expect(wrapper.find('[data-testid="sync-upstream-models"]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.grokModelDiagnostics.configuredFallback')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('upstream-sync-finished')).toEqual([[]])
    wrapper.unmount()
  })

  it('does not apply an older account sync to the current draft or clear its loading', async () => {
    const first = deferred<any>()
    const second = deferred<any>()
    adminAccountSyncMock.mockImplementation((id: number) => (id === 1 ? first.promise : second.promise))
    const wrapper = mountSelector({ platform: 'grok', accountId: 1, modelValue: ['a-local'] })
    await syncButton(wrapper).trigger('click')
    expect((syncButton(wrapper).element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.setProps({ accountId: 2, modelValue: ['b-local'] })
    expect((syncButton(wrapper).element as HTMLButtonElement).disabled).toBe(false)
    await syncButton(wrapper).trigger('click')
    expect(adminAccountSyncMock).toHaveBeenNthCalledWith(1, 1)
    expect(adminAccountSyncMock).toHaveBeenNthCalledWith(2, 2)
    expect((syncButton(wrapper).element as HTMLButtonElement).disabled).toBe(true)

    first.resolve({ models: ['a-private-catalog'], discovery_source: 'upstream' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.emitted('upstream-sync-finished')).toBeUndefined()
    expect((syncButton(wrapper).element as HTMLButtonElement).disabled).toBe(true)

    second.resolve({ models: ['b-catalog'], discovery_source: 'upstream' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['b-local', 'b-catalog']]])
    expect(showSuccess).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('upstream-sync-finished')).toHaveLength(1)
    expect((syncButton(wrapper).element as HTMLButtonElement).disabled).toBe(false)
  })

  it('does not toast or finish an older account sync that fails during a newer sync', async () => {
    const first = deferred<any>()
    const second = deferred<any>()
    adminAccountSyncMock.mockImplementation((id: number) => (id === 1 ? first.promise : second.promise))
    const wrapper = mountSelector({ platform: 'grok', accountId: 1, modelValue: ['a-local'] })
    await syncButton(wrapper).trigger('click')
    await wrapper.setProps({ accountId: 2, modelValue: ['b-local'] })
    await syncButton(wrapper).trigger('click')

    first.reject({ message: 'old account failure' })
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('upstream-sync-finished')).toBeUndefined()
    expect((syncButton(wrapper).element as HTMLButtonElement).disabled).toBe(true)

    second.reject({ message: 'current failure' })
    await flushPromises()
    expect(showError).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('upstream-sync-finished')).toHaveLength(1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect((syncButton(wrapper).element as HTMLButtonElement).disabled).toBe(false)
  })

  it('does not toast when a saved sync fails after the selector is destroyed', async () => {
    const pending = deferred<any>()
    adminAccountSyncMock.mockReturnValue(pending.promise)
    const wrapper = mountSelector({ platform: 'grok', accountId: 46, modelValue: ['grok-4.7'] })
    await syncButton(wrapper).trigger('click')
    wrapper.unmount()
    pending.reject({ message: 'old account failure' })
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
  })
})
