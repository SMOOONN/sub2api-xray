import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupSelector from '../GroupSelector.vue'
import type { AdminGroup } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const group = (id: number, ownerUserId: number | null | undefined, name: string) => ({
  id,
  ...(ownerUserId === undefined ? {} : { owner_user_id: ownerUserId }),
  name,
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  status: 'active',
  subscription_type: 'standard',
  account_count: 0,
} as AdminGroup)

const groups = [
  group(1, null, 'Explicit system group'),
  group(4, undefined, 'Implicit system group'),
  group(2, 7, 'User 7 group'),
  group(3, 8, 'User 8 group'),
]

const mountSelector = (ownerUserId: number | null | undefined) => mount(GroupSelector, {
  props: {
    modelValue: [],
    groups,
    platform: 'openai',
    enforceOwner: true,
    ownerUserId,
  },
  global: {
    stubs: {
      GroupBadge: {
        props: ['name'],
        template: '<span>{{ name }}</span>',
      },
      Icon: true,
    },
  },
})

describe('GroupSelector owner scope', () => {
  it('treats a missing group owner as system-owned when ownerUserId is null', () => {
    const text = mountSelector(null).text()
    expect(text).toContain('Explicit system group')
    expect(text).toContain('Implicit system group')
    expect(text).not.toContain('User 7 group')
    expect(text).not.toContain('User 8 group')
  })

  it('treats an undefined ownerUserId as the system owner', () => {
    const text = mountSelector(undefined).text()
    expect(text).toContain('Explicit system group')
    expect(text).toContain('Implicit system group')
    expect(text).not.toContain('User 7 group')
    expect(text).not.toContain('User 8 group')
  })

  it('keeps groups with a different private owner hidden', () => {
    const text = mountSelector(7).text()
    expect(text).toContain('User 7 group')
    expect(text).not.toContain('Explicit system group')
    expect(text).not.toContain('Implicit system group')
    expect(text).not.toContain('User 8 group')
  })
})
