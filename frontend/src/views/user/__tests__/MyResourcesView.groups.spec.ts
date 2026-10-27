import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import GroupI18nField from '@/components/admin/group/GroupI18nField.vue'
import MyResourcesView from '../MyResourcesView.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('vue-router', () => ({ useRoute: () => ({ meta: { resource: 'groups' }, query: {} }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn() }) }))
vi.mock('@/api/myResources', () => ({
  myResourcesApi: {
    groups: {
      list: mocks.list,
      create: mocks.create,
      update: mocks.update,
      usageSummary: vi.fn().mockResolvedValue([]),
      capacitySummary: vi.fn().mockResolvedValue([]),
      modelCandidates: vi.fn().mockResolvedValue({ models: [] }),
    },
    proxies: { sources: { list: vi.fn().mockResolvedValue({ items: [] }) } },
  },
}))

const TableStub = defineComponent({
  props: ['data'],
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>',
})
const SelectStub = defineComponent({
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
})

function buildGroup(platform = 'gemini', allowImageGeneration = false) {
  return {
    id: 7,
    name: 'Default group',
    description: 'Default description',
    i18n: { en: { name: 'English group', description: 'English description' } },
    platform,
    allow_image_generation: allowImageGeneration,
    subscription_type: 'subscription',
    status: 'active',
    rate_multiplier: 1,
  }
}

async function mountGroups(group = buildGroup()) {
  mocks.list.mockImplementation(async () => ({ items: [group], total: 1, page: 1, page_size: 20 }))
  const wrapper = shallowMount(MyResourcesView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
        DataTable: TableStub,
        Select: SelectStub,
        GroupI18nField: false,
      },
    },
  })
  await flushPromises()
  return wrapper
}

type GroupsWrapper = Awaited<ReturnType<typeof mountGroups>>

async function clickButton(wrapper: GroupsWrapper, text: string) {
  const button = wrapper.findAll('button').find(candidate => candidate.text() === text)
  expect(button, text).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

function field(wrapper: GroupsWrapper, name: 'name' | 'description') {
  return wrapper.findAllComponents(GroupI18nField).find(component => component.props('field') === name)!
}

async function selectEnglish(wrapper: GroupsWrapper, name: 'name' | 'description') {
  const component = field(wrapper, name)
  await component.get('[data-testid="group-i18n-menu"]').trigger('click')
  await component.get('[data-locale="en"]').trigger('click')
  return component
}

function imageGenerationCheckbox(wrapper: GroupsWrapper) {
  const label = wrapper.findAll('.fixed label').find(candidate => candidate.text() === 'myResources.fields.imageGeneration')
  expect(label).toBeDefined()
  return label!.get<HTMLInputElement>('input')
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  mocks.create.mockResolvedValue({ id: 8 })
  mocks.update.mockResolvedValue({ id: 7 })
})

describe('private group editor integration', () => {
  it('edits a translation without changing default text or the loaded group before saving', async () => {
    const group = buildGroup()
    const wrapper = await mountGroups(group)
    await clickButton(wrapper, 'myResources.actions.edit')
    const name = await selectEnglish(wrapper, 'name')
    expect(name.get<HTMLInputElement>('input').element.value).toBe('English group')
    await name.get('input').setValue('Updated translation')
    expect(group.i18n.en.name).toBe('English group')
    await clickButton(wrapper, 'common.save')
    expect(mocks.update).toHaveBeenCalledWith(7, expect.objectContaining({
      name: 'Default group',
      description: 'Default description',
      i18n: { en: { name: 'Updated translation', description: 'English description' } },
      allow_image_generation: false,
    }))
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
  })

  it('submits empty translated fields so existing translations can be cleared', async () => {
    const wrapper = await mountGroups()
    await clickButton(wrapper, 'myResources.actions.edit')
    await (await selectEnglish(wrapper, 'name')).get('input').setValue('')
    await (await selectEnglish(wrapper, 'description')).get('textarea').setValue('')
    await clickButton(wrapper, 'common.save')
    expect(mocks.update).toHaveBeenCalledWith(7, expect.objectContaining({
      i18n: { en: { name: '', description: '' } },
    }))
  })

  it.each(['gemini', 'antigravity', 'grok'])('defaults new %s groups to images and resets translations after cancel', async platform => {
    const wrapper = await mountGroups()
    await clickButton(wrapper, 'myResources.actions.edit')
    await (await selectEnglish(wrapper, 'name')).get('input').setValue('Cancelled translation')
    await clickButton(wrapper, 'common.cancel')
    await clickButton(wrapper, 'myResources.pages.groups.create')
    expect(field(wrapper, 'name').props('i18n')).toEqual({})
    await field(wrapper, 'name').get('input').setValue(' New group ')
    await wrapper.get('.fixed select').setValue(platform)
    expect(imageGenerationCheckbox(wrapper).element.checked).toBe(true)
    await clickButton(wrapper, 'common.save')
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      name: 'New group', platform, i18n: {}, allow_image_generation: true,
    }))
    expect(mocks.update).not.toHaveBeenCalled()
  })

  it('keeps other new platforms disabled and preserves a stored image setting when editing', async () => {
    const wrapper = await mountGroups(buildGroup('gemini', false))
    await clickButton(wrapper, 'myResources.pages.groups.create')
    for (const platform of ['gemini', 'openai', 'anthropic']) {
      await wrapper.get('.fixed select').setValue(platform)
      expect(imageGenerationCheckbox(wrapper).element.checked).toBe(platform === 'gemini')
    }
    await clickButton(wrapper, 'common.cancel')
    await clickButton(wrapper, 'myResources.actions.edit')
    expect(imageGenerationCheckbox(wrapper).element.checked).toBe(false)
    await wrapper.get('.fixed select').setValue('grok')
    expect(imageGenerationCheckbox(wrapper).element.checked).toBe(false)
  })
})
