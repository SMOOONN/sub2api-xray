import { apiClient } from '../client'

export type RetryFilterAction = 'same_account_retry' | 'switch_account' | 'terminate' | 'observe'
export type RetryFilterEmptyRequestMode = 'none' | 'body' | 'structured' | 'both'
export type RetryFilterEmptyResponseMode = 'none' | 'completed' | 'interrupted' | 'both'
export interface RetryFilterPreset {
  id: number
  name: string
  enabled: boolean
  description?: string | null
  created_at: string
  updated_at: string
}
export type RetryFilterPresetInput = Pick<RetryFilterPreset, 'name' | 'enabled' | 'description'>
export interface RetryFilterRule {
  id: number
  preset_id: number
  name: string
  enabled: boolean
  priority: number
  status_codes: number[]
  keywords: string[]
  upstream_codes: string[]
  upstream_code_paths: string[]
  models: string[]
  match_mode: 'any' | 'all'
  action: RetryFilterAction
  retry_count_override?: number | null
  retry_delay_ms?: number | null
  empty_request_mode?: RetryFilterEmptyRequestMode | null
  empty_response_mode?: RetryFilterEmptyResponseMode | null
  terminate_status_code?: number | null
  terminate_message?: string | null
  description?: string | null
  created_at: string
  updated_at: string
}
export type RetryFilterRuleInput = Omit<RetryFilterRule, 'id' | 'preset_id' | 'created_at' | 'updated_at'>
export interface RetryFilterBinding { id: number; name: string; preset_id: number | null; eligible?: boolean }
export interface RetryFilterBindingCandidate {
  id: number
  name: string
  preset_id?: number | null
  eligible?: boolean
  platform?: string
  status?: string
  email?: string
}
export interface RetryFilterBindingCandidates {
  items: RetryFilterBindingCandidate[]
  total: number
  page: number
  page_size: number
  pages?: number
}
export interface RetryFilterBindingAssignment {
  accounts?: number[]
  groups?: number[]
  global?: boolean
}
export interface RetryFilterPresetDetail {
  preset: RetryFilterPreset
  rules: RetryFilterRule[]
  bindings: {
    account_ids?: number[]
    group_ids?: number[]
    account_count?: number
    group_count?: number
    global?: boolean
  }
}
export interface RetryFilterDefault { preset_id: number | null; enabled: boolean }
export interface RetryFilterEffectiveSource {
  preset_id: number
  source: 'account' | 'group' | 'global' | 'none'
  enabled: boolean
  eligible: boolean
}
export interface RetryFilterMonitorEvent {
  id: number | string
  created_at: string
  request_id?: string | null
  action?: RetryFilterAction | string | null
  reason?: string | null
  rule_id?: number | null
  rule_name?: string | null
  preset_id?: number | null
  preset_name?: string | null
  status_code?: number | null
  model?: string | null
  account_id?: number | null
  account_name?: string | null
  group_id?: number | null
  group_name?: string | null
  retry_count?: number | null
  retry_delay_ms?: number | null
  empty_request?: boolean
  empty_response?: boolean
  kind?: string | null
  event_type?: string | null
  behavior_changed?: boolean
  detail?: string | null
  metadata?: Record<string, unknown> | null
}
export interface RetryFilterMonitorSummary {
  matched: number
  retries: number
  terminates: number
  switches?: number
  observations?: number
  by_action?: Record<string, number>
  recent_errors?: number | RetryFilterMonitorEvent[]
}
export interface RetryFilterMonitorQuery {
  page?: number
  page_size?: number
  start_time?: string
  end_time?: string
  action?: string
  status_code?: number | string
  model?: string
  account_id?: number | string
  group_id?: number | string
  preset_id?: number | string
  rule_id?: number | string
  reason?: string
  q?: string
}
export interface RetryFilterMonitorResponse {
  items: RetryFilterMonitorEvent[]
  total: number
  page: number
  page_size: number
  pages?: number
  summary?: RetryFilterMonitorSummary
}
const base = '/admin/retry-filter'
const retryFilterAPI = {
  async listPresets() { return (await apiClient.get<RetryFilterPreset[]>(`${base}/presets`)).data },
  async createPreset(input: RetryFilterPresetInput) { return (await apiClient.post<RetryFilterPreset>(`${base}/presets`, input)).data },
  async updatePreset(id: number, input: RetryFilterPresetInput) { return (await apiClient.put<RetryFilterPreset>(`${base}/presets/${id}`, input)).data },
  async deletePreset(id: number) { await apiClient.delete(`${base}/presets/${id}`) },
  async listRules(id: number) { return (await apiClient.get<RetryFilterRule[]>(`${base}/presets/${id}/rules`)).data },
  async getPresetDetail(id: number) { return (await apiClient.get<RetryFilterPresetDetail>(`${base}/presets/${id}/detail`)).data },
  async createRule(id: number, input: RetryFilterRuleInput) { return (await apiClient.post<RetryFilterRule>(`${base}/presets/${id}/rules`, input)).data },
  async updateRule(id: number, ruleID: number, input: RetryFilterRuleInput) { return (await apiClient.put<RetryFilterRule>(`${base}/presets/${id}/rules/${ruleID}`, input)).data },
  async deleteRule(id: number, ruleID: number) { await apiClient.delete(`${base}/presets/${id}/rules/${ruleID}`) },
  async reorderRules(id: number, ruleIDs: number[]) {
    return (await apiClient.put<RetryFilterRule[]>(`${base}/presets/${id}/rules/reorder`, { rule_ids: ruleIDs })).data
  },
  async getDefault() { return (await apiClient.get<RetryFilterDefault>(`${base}/default`)).data },
  async setDefault(preset_id: number | null) { return (await apiClient.put<RetryFilterDefault>(`${base}/default`, { preset_id })).data },
  async getBinding(kind: 'accounts' | 'groups', id: number) { return (await apiClient.get<RetryFilterBinding>(`${base}/bindings/${kind}/${id}`)).data },
  async setBinding(kind: 'accounts' | 'groups', id: number, preset_id: number | null) { return (await apiClient.put<RetryFilterBinding>(`${base}/bindings/${kind}/${id}`, { preset_id })).data },
  async effectiveSource(account_id?: number, group_id?: number) { return (await apiClient.get<RetryFilterEffectiveSource>(`${base}/effective-source`, { params: { account_id, group_id } })).data },
  async listBindingCandidates(kind: 'accounts' | 'groups', params: { q?: string; page?: number; page_size?: number; preset_id?: number }) {
    return (await apiClient.get<RetryFilterBindingCandidates>(`${base}/binding-candidates/${kind}`, { params })).data
  },
  async getPresetBindings(presetID: number) {
    const detail = (await apiClient.get<RetryFilterPresetDetail>(`${base}/presets/${presetID}/detail`)).data
    return { accounts: detail.bindings.account_ids || [], groups: detail.bindings.group_ids || [], global: detail.bindings.global || false }
  },
  async setPresetBindings(presetID: number, input: RetryFilterBindingAssignment) {
    return (await apiClient.put<RetryFilterBindingAssignment>(`${base}/presets/${presetID}/bindings`, input)).data
  },
  async listMonitor(params: RetryFilterMonitorQuery = {}) {
    return (await apiClient.get<RetryFilterMonitorResponse>(`${base}/monitor`, { params })).data
  },
  async getMonitorEvent(id: number | string) {
    return (await apiClient.get<RetryFilterMonitorEvent>(`${base}/monitor/events/${encodeURIComponent(String(id))}`)).data
  }
}
export default retryFilterAPI
