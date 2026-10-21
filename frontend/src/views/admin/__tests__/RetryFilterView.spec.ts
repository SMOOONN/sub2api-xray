import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import RetryFilterView from '../RetryFilterView.vue'

const {
  listPresets,
  getDefault,
  listRules,
  getPresetBindings,
  listBindingCandidates,
  setPresetBindings,
  createPreset,
  updatePreset,
  deletePreset,
  createRule,
  updateRule,
  deleteRule,
  reorderRules,
  showSuccess
} = vi.hoisted(() => ({
  listPresets: vi.fn(),
  getDefault: vi.fn(),
  listRules: vi.fn(),
  getPresetBindings: vi.fn(),
  listBindingCandidates: vi.fn(),
  setPresetBindings: vi.fn(),
  createPreset: vi.fn(),
  updatePreset: vi.fn(),
  deletePreset: vi.fn(),
  createRule: vi.fn(),
  updateRule: vi.fn(),
  deleteRule: vi.fn(),
  reorderRules: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin/retryFilter', () => ({
  default: {
    listPresets,
    getDefault,
    listRules,
    getPresetBindings,
    listBindingCandidates,
    setPresetBindings,
    createPreset,
    updatePreset,
    deletePreset,
    createRule,
    updateRule,
    deleteRule,
    reorderRules
  }
}))

vi.mock('@/api/admin/accounts', () => ({ default: { list: vi.fn() } }))
vi.mock('@/api/admin/groups', () => ({ default: { list: vi.fn() } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const AppLayoutStub = defineComponent({ template: '<div><slot /></div>' })
const RouterLinkStub = defineComponent({ template: '<a><slot /></a>' })
const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false },
    title: { type: String, default: '' },
    width: { type: String, default: 'normal' }
  },
  template: '<div v-if="show" data-test="base-dialog" :data-title="title" :data-width="width"><slot /><slot name="footer" /></div>'
})

const preset = { id: 7, name: 'Pool policy', enabled: true, description: '', created_at: '', updated_at: '' }
const rule = {
  id: 21,
  preset_id: 7,
  name: 'Rate limited',
  enabled: true,
  priority: 0,
  status_codes: [429],
  keywords: [],
  upstream_codes: [],
  upstream_code_paths: [],
  models: [],
  match_mode: 'any' as const,
  action: 'same_account_retry' as const,
  empty_request_mode: 'none' as const,
  empty_response_mode: 'none' as const,
  created_at: '',
  updated_at: ''
}

function mountView() {
  return mount(RetryFilterView, {
    global: {
      stubs: { AppLayout: AppLayoutStub, RouterLink: RouterLinkStub, BaseDialog: BaseDialogStub, RetryFilterGuide: true }
    }
  })
}

enableAutoUnmount(afterEach)

async function openNewRule(wrapper: ReturnType<typeof mountView>) {
  await wrapper.findAll('button').find(button => button.text() === 'admin.retryFilter.addRule')!.trigger('click')
  const form = wrapper.get('#retry-filter-rule-editor')
  await form.get('[name="name"]').setValue('New rule')
  return form
}

describe('admin RetryFilterView', () => {
  beforeEach(() => {
    listPresets.mockReset().mockResolvedValue([preset])
    getDefault.mockReset().mockResolvedValue({ preset_id: null, enabled: true })
    listRules.mockReset().mockImplementation(async () => [{ ...rule, status_codes: [...rule.status_codes] }])
    getPresetBindings.mockReset().mockResolvedValue({ accounts: [], groups: [], global: false })
    listBindingCandidates.mockReset().mockResolvedValue({
      items: [{ id: 314, name: 'Pool account', platform: 'openai', eligible: true }],
      total: 1,
      pages: 1
    })
    setPresetBindings.mockReset().mockResolvedValue({})
    createPreset.mockReset().mockImplementation(async input => ({ ...preset, ...input, id: 9 }))
    updatePreset.mockReset().mockImplementation(async (_id, input) => ({ ...preset, ...input }))
    deletePreset.mockReset().mockResolvedValue(undefined)
    createRule.mockReset().mockResolvedValue({ ...rule, id: 22 })
    updateRule.mockReset().mockResolvedValue(rule)
    deleteRule.mockReset().mockResolvedValue(undefined)
    reorderRules.mockReset().mockResolvedValue([])
    showSuccess.mockReset()
  })

  it('opens add and edit forms in a wide dialog, saves the preset switch, and shows candidate IDs', async () => {
    const wrapper = mountView()
    await flushPromises()

    const addRule = wrapper.findAll('button').find(button => button.text() === 'admin.retryFilter.addRule')
    expect(addRule).toBeDefined()
    await addRule!.trigger('click')

    let dialog = wrapper.get('[data-test="base-dialog"]')
    expect(dialog.attributes('data-title')).toBe('admin.retryFilter.addRule')
    expect(dialog.attributes('data-width')).toBe('extra-wide')
    expect(dialog.find('form#retry-filter-rule-editor').exists()).toBe(true)
    await dialog.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')
    expect(wrapper.find('[data-test="base-dialog"]').exists()).toBe(false)

    await addRule!.trigger('click')
    const form = wrapper.get('form#retry-filter-rule-editor')
    await form.get('[name="name"]').setValue('New rule')
    await form.get('[name="statusText"]').setValue('429')
    await form.trigger('submit')
    await flushPromises()
    expect(createRule).toHaveBeenCalledWith(7, expect.objectContaining({ name: 'New rule', status_codes: [429] }))
    expect(wrapper.find('[data-test="base-dialog"]').exists()).toBe(false)

    await wrapper.get('button[title="common.edit"]').trigger('click')
    dialog = wrapper.get('[data-test="base-dialog"]')
    expect(dialog.attributes('data-title')).toBe('admin.retryFilter.editRule')
    await dialog.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')

    const presetSwitch = wrapper.get('[role="switch"]')
    expect(presetSwitch.attributes('aria-checked')).toBe('true')
    await presetSwitch.trigger('click')
    expect(presetSwitch.attributes('aria-checked')).toBe('false')
    await wrapper.findAll('button').find(button => button.text() === 'admin.retryFilter.savePreset')!.trigger('click')
    await flushPromises()
    expect(updatePreset).toHaveBeenCalledWith(7, expect.objectContaining({ enabled: false }))

    expect(wrapper.text()).toContain('admin.retryFilter.idLabel')
    expect(wrapper.text()).toContain('314')
    expect(wrapper.text()).toContain('admin.retryFilter.platform')
    expect(wrapper.text()).toContain('admin.retryFilter.status')
    expect(wrapper.text()).toContain('openai')
  })

  it.each([
    {
      input: '400-410,500-510',
      codes: [400, 401, 402, 403, 404, 405, 406, 407, 408, 409, 410, 500, 501, 502, 503, 504, 505, 506, 507, 508, 509, 510]
    },
    { input: '503 429,500-502,501,429 502-504', codes: [429, 500, 501, 502, 503, 504] },
    { input: ' \t400 - 402,\n500 - 502 \t503\n599 ', codes: [400, 401, 402, 500, 501, 502, 503, 599] },
    { input: '100,599', codes: [100, 599] },
    { input: '429-429 429', codes: [429] },
    { input: '100-599', codes: Array.from({ length: 500 }, (_, index) => index + 100) }
  ])('saves all codes for "$input" in sorted, unique form', async ({ input, codes }) => {
    const wrapper = mountView()
    await flushPromises()
    const form = await openNewRule(wrapper)

    expect(form.get('[name="statusText"]').attributes('placeholder')).toBe('admin.retryFilter.statusPlaceholder')
    expect(form.get('#retry-filter-status-hint').text()).toBe('admin.retryFilter.statusHint')
    await form.get('[name="statusText"]').setValue(input)
    await form.trigger('submit')
    await flushPromises()

    expect(createRule).toHaveBeenCalledTimes(1)
    expect(createRule).toHaveBeenCalledWith(7, expect.objectContaining({ status_codes: codes }))
    expect(wrapper.find('[data-test="base-dialog"]').exists()).toBe(false)
  })

  it.each(['new', 'existing'])('blocks the entire %s rule save on invalid status input, then allows a correction', async mode => {
    const wrapper = mountView()
    await flushPromises()
    if (mode === 'new') await openNewRule(wrapper)
    else await wrapper.get('button[title="common.edit"]').trigger('click')
    const form = wrapper.get('#retry-filter-rule-editor')
    // A valid second condition must never cause an invalid status entry to be ignored.
    await form.get('[name="keywordsText"]').setValue('overloaded')

    for (const input of [
      '410-400', '99', '600', '99-101', '598-600',
      '400-', '-410', '400--410', '400-410-420', '400-,500', '429,500 -',
      'foo', '429,500-50x,503', '400 41a 500', '1e2', '4e2-410',
      '400.0', '400.5-410', '+400', '0x190', '0400',
      '429,,500', '429, ,500', ',', '429,', ',429', '400;410'
    ]) {
      await form.get('[name="statusText"]').setValue(input)
      await form.trigger('submit')
      await flushPromises()

      expect(createRule, input).not.toHaveBeenCalled()
      expect(updateRule, input).not.toHaveBeenCalled()
      expect(form.get('[name="statusText"]').attributes('aria-invalid'), input).toBe('true')
      expect(form.get('#retry-filter-status-error').attributes('role')).toBe('alert')
      expect(form.get('#retry-filter-status-error').text()).toBe('admin.retryFilter.invalidStatusCodes')
      expect(wrapper.find('[data-test="base-dialog"]').exists()).toBe(true)
    }

    await form.get('[name="statusText"]').setValue('500 - 502')
    expect(form.find('#retry-filter-status-error').exists()).toBe(false)
    expect(form.get('[name="statusText"]').attributes('aria-invalid')).toBe('false')
    await form.trigger('submit')
    await flushPromises()

    const payload = expect.objectContaining({ status_codes: [500, 501, 502], keywords: ['overloaded'] })
    if (mode === 'new') expect(createRule).toHaveBeenCalledWith(7, payload)
    else expect(updateRule).toHaveBeenCalledWith(7, 21, payload)
    expect(wrapper.find('[data-test="base-dialog"]').exists()).toBe(false)
  })

  it('renders compact ranges and preserves rule semantics through an edit round trip', async () => {
    const originalCodes = [502, 400, 401, 402, 429, 500, 501, 502]
    const existingRule = {
      ...rule,
      status_codes: originalCodes,
      keywords: ['overloaded'],
      upstream_codes: ['rate_limit_exceeded'],
      upstream_code_paths: ['error.code'],
      models: ['gpt-5*'],
      match_mode: 'all' as const,
      retry_count_override: 2,
      retry_delay_ms: 1500,
      description: 'Recover transient errors'
    }
    listRules.mockResolvedValue([existingRule])
    const wrapper = mountView()
    await flushPromises()

    const card = wrapper.get('[data-rule-id="21"]')
    expect(card.text()).toContain('HTTP 400-402, 429, 500-502')
    expect(card.text()).toContain('rate_limit_exceeded')
    await card.get('button[title="common.edit"]').trigger('click')
    const form = wrapper.get('#retry-filter-rule-editor')
    expect((form.get('[name="statusText"]').element as HTMLTextAreaElement).value).toBe('400-402, 429, 500-502')
    await form.trigger('submit')
    await flushPromises()

    expect(updateRule).toHaveBeenCalledWith(7, 21, expect.objectContaining({
      status_codes: [400, 401, 402, 429, 500, 501, 502],
      name: existingRule.name,
      enabled: true,
      action: 'same_account_retry',
      match_mode: 'all',
      keywords: ['overloaded'],
      upstream_codes: ['rate_limit_exceeded'],
      upstream_code_paths: ['error.code'],
      models: ['gpt-5*'],
      retry_count_override: 2,
      retry_delay_ms: 1500,
      description: 'Recover transient errors'
    }))
    expect(existingRule.status_codes).toEqual([502, 400, 401, 402, 429, 500, 501, 502])
    expect(createRule).not.toHaveBeenCalled()
  })

  it.each([
    { field: 'keywordsText', value: 'overloaded', payload: { keywords: ['overloaded'] } },
    { field: 'emptyResponseMode', value: 'completed', payload: { empty_response_mode: 'completed' } }
  ])('keeps blank statuses empty and accepts the $field condition', async ({ field, value, payload }) => {
    const wrapper = mountView()
    await flushPromises()
    const form = await openNewRule(wrapper)
    await form.get('[name="statusText"]').setValue(' \n\t ')
    await form.get('[name="pathsText"]').setValue('error.code')
    await form.trigger('submit')
    await flushPromises()

    expect(createRule).not.toHaveBeenCalled()
    expect(form.get('[role="alert"]').text()).toBe('admin.retryFilter.conditionRequired')
    expect(form.find('#retry-filter-status-error').exists()).toBe(false)
    if (field === 'emptyResponseMode') {
      await form.get('#retry-filter-emptyResponseMode').trigger('click')
      const option = Array.from(document.querySelectorAll('[role="option"]'))
        .find(element => element.textContent?.trim() === 'admin.retryFilter.emptyModes.' + value)
      expect(option).toBeDefined()
      ;(option as HTMLElement).click()
      await flushPromises()
      expect(form.get('#retry-filter-emptyResponseMode').text()).toContain('admin.retryFilter.emptyModes.' + value)
    } else {
      await form.get('[name="' + field + '"]').setValue(value)
    }
    await form.trigger('submit')
    await flushPromises()

    expect(createRule).toHaveBeenCalledWith(7, expect.objectContaining({
      status_codes: [],
      upstream_code_paths: ['error.code'],
      ...payload
    }))
  })

  it('retains rule ordering, duplication, toggles, and confirmed deletion', async () => {
    const longName = 'Upstream temporarily overloaded '.repeat(12).trim()
    const longCode = 'upstream_error_'.repeat(24)
    let currentRules = [
      { ...rule },
      { ...rule, id: 23, name: longName, priority: 1, status_codes: [503], upstream_codes: [longCode] }
    ]
    listRules.mockImplementation(async () => currentRules.map(item => ({ ...item, status_codes: [...item.status_codes] })))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-rule-id="23"]').text()).toContain(longName)
    expect(wrapper.get('[data-rule-id="23"]').text()).toContain(longCode)
    await wrapper.get('[data-rule-id="23"]').trigger('dragstart')
    await wrapper.get('[data-rule-id="21"]').trigger('drop')
    await flushPromises()
    expect(reorderRules).toHaveBeenCalledWith(7, [23, 21])
    expect(wrapper.findAll('[data-rule-id]')[0].attributes('data-rule-id')).toBe('23')

    await wrapper.get('[data-rule-id="21"] button[title="admin.retryFilter.duplicate"]').trigger('click')
    await flushPromises()
    expect(createRule).toHaveBeenCalledWith(7, expect.objectContaining({
      name: 'Rate limited (admin.retryFilter.copySuffix)',
      status_codes: [429],
      action: 'same_account_retry'
    }))

    currentRules = currentRules.map(item => item.id === 21 ? { ...item, enabled: false } : item)
    await wrapper.get('[data-rule-id="21"] [role="switch"]').trigger('click')
    await flushPromises()
    expect(updateRule).toHaveBeenCalledWith(7, 21, expect.objectContaining({ enabled: false, status_codes: [429] }))
    expect(wrapper.get('[data-rule-id="21"] [role="switch"]').attributes('aria-checked')).toBe('false')

    await wrapper.get('[data-rule-id="21"] button[title="common.delete"]').trigger('click')
    expect(deleteRule).not.toHaveBeenCalled()
    const dialog = wrapper.get('[data-test="base-dialog"]')
    await dialog.findAll('button').find(button => button.text() === 'common.delete')!.trigger('click')
    await flushPromises()
    expect(deleteRule).toHaveBeenCalledWith(7, 21)
    expect(wrapper.find('[data-rule-id="21"]').exists()).toBe(false)
    expect(wrapper.find('[data-rule-id="23"]').exists()).toBe(true)
  })

  it('retains selections across binding searches, pages and kinds, and saves the combined scope', async () => {
    const groupOne = { id: 314, name: 'Team one', platform: 'openai', eligible: true }
    const groupTwo = { id: 315, name: 'Long group name '.repeat(20).trim(), platform: 'anthropic', eligible: true }
    const account = { id: 901, name: 'Pool account', platform: 'openai', eligible: true }
    const outsideScope = { id: 902, name: 'OAuth account', platform: 'anthropic', eligible: false }
    listBindingCandidates.mockImplementation(async (kind, params) => ({
      items: kind === 'accounts' ? [account, outsideScope] : params.page === 2 ? [groupTwo] : [groupOne],
      page: params.page,
      page_size: 12,
      total: kind === 'accounts' ? 2 : 13,
      pages: kind === 'accounts' ? 1 : 2
    }))
    const wrapper = mountView()
    await flushPromises()
    const scope = wrapper.get('section[aria-labelledby="retry-filter-scope-title"]')
    await scope.get('input[aria-label="admin.retryFilter.searchBinding"]').setValue('team')
    await scope.get('input[aria-label="admin.retryFilter.searchBinding"]').trigger('keyup', { key: 'Enter' })
    await flushPromises()
    expect(listBindingCandidates).toHaveBeenLastCalledWith('groups', {
      q: 'team', page: 1, page_size: 12, preset_id: 7
    })

    await scope.get('[data-candidate-id="314"] input').setValue(true)
    await scope.get('button[aria-label="pagination.next"]').trigger('click')
    await flushPromises()
    expect(listBindingCandidates).toHaveBeenLastCalledWith('groups', {
      q: 'team', page: 2, page_size: 12, preset_id: 7
    })
    await scope.get('[data-candidate-id="315"] input').setValue(true)
    expect(scope.get('[data-candidate-id="315"]').text()).toContain(groupTwo.name)

    await scope.get('[data-binding-kind="accounts"]').trigger('click')
    await flushPromises()
    expect(scope.get('[data-binding-kind="accounts"]').attributes('aria-pressed')).toBe('true')
    expect(listBindingCandidates).toHaveBeenLastCalledWith('accounts', {
      q: 'team', page: 1, page_size: 12, preset_id: 7
    })
    expect(scope.get('[data-candidate-id="901"]').text()).toContain('openai')
    expect(scope.get('[data-candidate-id="902"]').text()).toContain('admin.retryFilter.notEligible')
    await scope.get('[data-candidate-id="901"] input').setValue(true)

    await scope.get('[data-binding-kind="groups"]').trigger('click')
    await flushPromises()
    expect((scope.get('[data-candidate-id="314"] input').element as HTMLInputElement).checked).toBe(true)
    await scope.get('button[aria-label="admin.retryFilter.removeBinding"]').trigger('click')
    expect((scope.get('[data-candidate-id="314"] input').element as HTMLInputElement).checked).toBe(false)
    expect(scope.text()).toContain(groupTwo.name)
    await scope.get('input[aria-describedby="retry-filter-default-hint"]').setValue(true)
    await scope.findAll('button').find(button => button.text() === 'admin.retryFilter.saveBindings')!.trigger('click')
    await flushPromises()

    expect(setPresetBindings).toHaveBeenCalledWith(7, { accounts: [901], groups: [315], global: true })
  })

  it('shows a visible error for an empty rule set name and saves a corrected name', async () => {
    const wrapper = mountView()
    await flushPromises()
    const details = wrapper.get('section[aria-labelledby="retry-filter-preset-title"]')
    const save = details.findAll('button').find(button => button.text() === 'admin.retryFilter.savePreset')!
    await details.get('input').setValue('')
    await save.trigger('click')
    await flushPromises()
    expect(updatePreset).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.retryFilter.nameRequired')

    await details.get('input').setValue('Renamed policy')
    await save.trigger('click')
    await flushPromises()
    expect(updatePreset).toHaveBeenCalledWith(7, expect.objectContaining({ name: 'Renamed policy' }))
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('retains rule set search, creation, duplication, and confirmed deletion', async () => {
    listPresets.mockResolvedValue([preset, { ...preset, id: 8, name: 'Alternate policy' }])
    const wrapper = mountView()
    await flushPromises()
    const search = wrapper.get('input[aria-label="admin.retryFilter.searchRuleSets"]')
    await search.setValue('alternate')
    expect(wrapper.findAll('aside button[aria-pressed]')).toHaveLength(1)
    await wrapper.get('aside button[aria-pressed]').trigger('click')
    await flushPromises()
    expect(listRules).toHaveBeenLastCalledWith(8)
    await search.setValue('')

    await wrapper.get('aside').findAll('button').find(button => button.text() === 'common.add')!.trigger('click')
    await flushPromises()
    expect(createPreset).toHaveBeenCalledWith({ name: 'admin.retryFilter.newRuleSet', description: '', enabled: true })
    const details = wrapper.get('section[aria-labelledby="retry-filter-preset-title"]')
    createPreset.mockResolvedValueOnce({ ...preset, id: 10, name: 'Copied policy' })
    await details.findAll('button').find(button => button.text() === 'admin.retryFilter.duplicate')!.trigger('click')
    await flushPromises()
    expect(createRule).toHaveBeenCalledWith(10, expect.objectContaining({ name: 'Rate limited', status_codes: [429] }))

    await details.findAll('button').find(button => button.text() === 'common.delete')!.trigger('click')
    expect(deletePreset).not.toHaveBeenCalled()
    const dialog = wrapper.get('[data-test="base-dialog"]')
    await dialog.findAll('button').find(button => button.text() === 'common.delete')!.trigger('click')
    await flushPromises()
    expect(deletePreset).toHaveBeenCalledWith(10)
    expect(wrapper.find('[data-test="base-dialog"]').exists()).toBe(false)
  })
})
