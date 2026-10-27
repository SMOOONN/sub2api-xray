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

function syncButton(wrapper: ReturnType<typeof mount>) {
  const button = wrapper
    .findAll('button')
    .find(candidate => candidate.text() === 'admin.accounts.syncUpstreamModels')
  if (!button) throw new Error('sync upstream models button not found')
  return button
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
})
