<template>
  <AppLayout>
    <div class="-mt-2 space-y-4 pb-6">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.retryFilter.manageTitle') }}</h1>
        <div class="flex flex-wrap gap-2"><RetryFilterGuide /><button class="btn btn-secondary btn-sm" :disabled="loading || busy || rulesLoading" @click="requestRefresh"><Icon name="refresh" size="sm" />{{ t('common.refresh') }}</button><router-link class="btn btn-secondary btn-sm" to="/admin/retry-filter/monitor">{{ t('admin.retryFilter.monitorLink') }}</router-link><router-link class="btn btn-secondary btn-sm" to="/admin/settings">{{ t('admin.retryFilter.settings') }}</router-link></div>
      </header>
      <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <div v-if="loading" role="status" class="py-12 text-center text-gray-500">{{ t('common.loading') }}</div>
      <div v-else class="retry-workspace card overflow-hidden">
        <aside class="retry-presets min-w-0 border-b border-gray-200 bg-gray-50/70 p-4 lg:border-b-0 lg:border-r dark:border-dark-700 dark:bg-dark-900/30" :aria-label="t('admin.retryFilter.ruleSets')">
          <div class="mb-4 flex items-center justify-between gap-3"><h2 class="text-sm font-semibold">{{ t('admin.retryFilter.ruleSets') }} <span class="ml-1 text-xs font-normal text-gray-500">{{ presets.length }}</span></h2><button class="btn btn-secondary btn-sm" :disabled="busy || rulesLoading" :title="t('admin.retryFilter.addRuleSet')" @click="requestNewPreset"><Icon name="plus" size="sm" /><span class="sr-only">{{ t('common.add') }}</span></button></div>
          <input v-model="presetSearch" class="input mb-3" :aria-label="t('admin.retryFilter.searchRuleSets')" :placeholder="t('admin.retryFilter.searchRuleSets')" />
          <div class="retry-presets-list space-y-1.5">
            <p v-if="!filteredPresets.length" class="p-3 text-xs text-gray-500">{{ t(presets.length ? 'admin.retryFilter.noSearchResults' : 'admin.retryFilter.noPresets') }}</p>
            <button v-for="preset in filteredPresets" :key="preset.id" :disabled="busy || rulesLoading" :aria-pressed="selectedID === preset.id" class="w-full rounded-xl border p-3 text-left transition-colors disabled:cursor-wait" :class="selectedID === preset.id ? 'border-primary-300 bg-white shadow-sm dark:border-primary-700 dark:bg-dark-800' : 'border-transparent hover:bg-gray-100 dark:hover:bg-dark-700'" @click="selectPreset(preset.id)">
              <span class="block break-words text-sm font-medium [overflow-wrap:anywhere]">{{ preset.name }}</span>
              <span class="mt-2 flex flex-wrap items-center gap-2 text-xs text-gray-500"><span class="h-1.5 w-1.5 rounded-full" :class="preset.enabled ? 'bg-primary-500' : 'bg-gray-400'"></span>{{ preset.enabled ? t('admin.retryFilter.enabled') : t('admin.retryFilter.disabled') }}<span v-if="ruleCounts[preset.id] != null">· {{ ruleCounts[preset.id] }} {{ t('admin.retryFilter.rules') }}</span><span v-if="defaultID === preset.id" class="text-primary-700 dark:text-primary-300">{{ t('admin.retryFilter.defaultShort') }}</span></span>
            </button>
          </div>
          <p class="mt-5 hidden text-xs leading-relaxed text-gray-500 lg:block">{{ t('admin.retryFilter.ruleSetsHint') }}</p>
        </aside>
        <div v-if="selectedPreset" class="retry-main min-w-0">
          <div class="shrink-0 px-4 pt-5 sm:px-6">
            <div class="flex items-start justify-between gap-4">
              <div class="min-w-0"><div class="flex flex-wrap items-center gap-2"><h2 class="line-clamp-2 break-words text-lg font-semibold [overflow-wrap:anywhere]" :title="selectedPreset.name">{{ selectedPreset.name }}</h2><span v-if="defaultID === selectedID" class="rounded-md bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ t('admin.retryFilter.defaultShort') }}</span></div><p v-if="selectedPreset.description" :title="selectedPreset.description" class="mt-1.5 line-clamp-2 break-words text-xs leading-relaxed text-gray-500 [overflow-wrap:anywhere] dark:text-gray-400">{{ selectedPreset.description }}</p><p class="mt-3 text-xs text-gray-500">{{ t('admin.retryFilter.setSummary', { rules: rules.length, accounts: initialAccountIDs.size, groups: initialGroupIDs.size }) }}</p></div>
              <div class="flex shrink-0 items-center gap-2 pt-1"><span class="hidden text-xs text-gray-500 sm:inline">{{ selectedPreset.enabled ? t('admin.retryFilter.enabled') : t('admin.retryFilter.disabled') }}</span><Toggle :model-value="selectedPreset.enabled" :disabled="busy || rulesLoading" :aria-label="t('admin.retryFilter.enablePreset')" @update:model-value="togglePreset" /></div>
            </div>
            <div class="mt-5 flex gap-5 border-b border-gray-200 sm:gap-7 dark:border-dark-700" role="tablist" :aria-label="t('admin.retryFilter.manageTitle')" @keydown="navigateTabs">
              <button v-for="tab in tabs" :id="tab.titleID" :key="tab.key" role="tab" :aria-controls="`retry-filter-panel-${tab.key}`" :aria-selected="activeTab === tab.key" :tabindex="activeTab === tab.key ? 0 : -1" class="flex items-center gap-2 border-b-2 pb-3 text-sm" :class="activeTab === tab.key ? 'border-primary-600 font-semibold text-primary-700 dark:text-primary-300' : 'border-transparent text-gray-500 hover:text-gray-800 dark:hover:text-gray-200'" @click="activeTab = tab.key">
                {{ t(`admin.retryFilter.${tab.label}`) }}<span v-if="tab.key === 'rules'" class="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-normal dark:bg-dark-700">{{ rules.length }}</span><span v-if="tab.key === 'scope' ? bindingsDirty : tab.key === 'settings' && presetDirty" class="h-1.5 w-1.5 rounded-full bg-amber-500" :aria-label="t('admin.retryFilter.unsaved')"></span>
              </button>
            </div>
          </div>
          <div v-if="rulesLoading" role="status" class="p-10 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
          <template v-else>
            <section v-show="activeTab === 'rules'" id="retry-filter-panel-rules" role="tabpanel" aria-labelledby="retry-filter-rules-title" class="retry-panel retry-rules px-4 py-5 sm:px-6">
              <p v-if="!selectedPreset.enabled" class="mb-4 rounded-lg bg-amber-50 p-3 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.retryFilter.presetDisabledHint') }}</p>
              <div class="mb-4 flex shrink-0 flex-wrap items-center justify-between gap-3"><input v-if="!sorting" v-model="ruleSearch" class="input min-w-0 flex-1 basis-40 sm:max-w-xs" :aria-label="t('admin.retryFilter.searchRules')" :placeholder="t('admin.retryFilter.searchRules')" /><p v-else class="text-xs text-primary-700 dark:text-primary-300">{{ t('admin.retryFilter.sortHint') }}</p><div class="flex gap-2"><button class="btn btn-secondary btn-sm" :disabled="busy || rules.length < 2" :aria-pressed="sorting" @click="toggleSorting"><Icon :name="sorting ? 'check' : 'arrowsUpDown'" size="sm" />{{ t(`admin.retryFilter.${sorting ? 'finishSorting' : 'sortRules'}`) }}</button><button v-if="!sorting" class="btn btn-primary btn-sm" :disabled="busy" @click="beginRule()"><Icon name="plus" size="sm" />{{ t('admin.retryFilter.addRule') }}</button></div></div>
              <div class="retry-rule-row retry-rule-head border-b border-gray-200 px-2 pb-3 text-xs text-gray-500 dark:border-dark-700" aria-hidden="true"><span>#</span><span>{{ t('admin.retryFilter.ruleAndConditions') }}</span><span>{{ t('admin.retryFilter.action') }}</span><span>{{ t('admin.retryFilter.status') }}</span><span></span></div>
              <div class="retry-rule-list">
                <div v-if="!filteredRules.length" class="py-12 text-center"><p class="text-sm font-medium">{{ t(rules.length ? 'admin.retryFilter.noSearchResults' : 'admin.retryFilter.noRules') }}</p><button v-if="ruleSearch" class="btn btn-secondary btn-sm mt-4" @click="ruleSearch = ''">{{ t('admin.retryFilter.clearSearch') }}</button></div>
                <article v-for="rule in filteredRules" :key="rule.id" :data-rule-id="rule.id" class="retry-rule-row border-b border-gray-100 px-2 py-4 last:border-b-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-900/30" :class="{ 'opacity-65': !rule.enabled, 'cursor-grab': sorting }" :draggable="sorting && !busy" @dragstart="startDrag(rules.indexOf(rule))" @dragover.prevent @drop="dropRule(rules.indexOf(rule))" @dragend="draggedIndex = null">
                  <div class="retry-order flex flex-col items-center gap-1 text-xs text-gray-400"><button v-if="sorting" class="rounded p-1 hover:bg-gray-100 dark:hover:bg-dark-700" :disabled="busy || rules.indexOf(rule) === 0" :aria-label="t('admin.retryFilter.moveUp', { name: rule.name })" @click="moveRule(rule, -1)"><Icon name="chevronUp" size="sm" /></button><span class="font-mono">{{ rules.indexOf(rule) + 1 }}</span><button v-if="sorting" class="rounded p-1 hover:bg-gray-100 dark:hover:bg-dark-700" :disabled="busy || rules.indexOf(rule) === rules.length - 1" :aria-label="t('admin.retryFilter.moveDown', { name: rule.name })" @click="moveRule(rule, 1)"><Icon name="chevronDown" size="sm" /></button></div>
                  <button class="retry-rule-info min-w-0 text-left" :disabled="busy" @click="beginRule(rule)"><span class="block break-words text-sm font-medium [overflow-wrap:anywhere]">{{ rule.name }}</span><span class="mt-1.5 block break-words text-xs leading-relaxed text-gray-500 [overflow-wrap:anywhere] dark:text-gray-400">{{ ruleSummary(rule).join(rule.match_mode === 'all' ? ' ＋ ' : ' / ') || t('admin.retryFilter.noConditions') }}</span></button>
                  <div class="retry-action min-w-0"><span class="inline-block max-w-full rounded-md px-2 py-1 text-xs" :class="actionClasses[rule.action]">{{ t(`admin.retryFilter.actions.${rule.action}`) }}</span><p v-if="rule.action === 'same_account_retry' && (rule.retry_count_override != null || rule.retry_delay_ms != null)" class="mt-1 text-xs text-gray-500">{{ t('admin.retryFilter.retrySummary', { count: rule.retry_count_override ?? '—', delay: rule.retry_delay_ms ?? '—' }) }}</p></div>
                  <div class="retry-status flex items-center gap-2"><Toggle :model-value="rule.enabled" :disabled="busy" :aria-label="t('admin.retryFilter.ruleEnabled', { name: rule.name })" @update:model-value="toggleRule(rule)" /><span class="hidden text-xs text-gray-500 2xl:inline">{{ rule.enabled ? t('admin.retryFilter.enabled') : t('admin.retryFilter.disabled') }}</span></div>
                  <div class="retry-rule-tools flex items-center justify-end gap-1"><button class="retry-icon-button" :disabled="busy" :title="t('common.edit')" :aria-label="t('admin.retryFilter.editNamedRule', { name: rule.name })" @click="beginRule(rule)"><Icon name="edit" size="sm" /></button><div class="relative"><button class="retry-icon-button" :disabled="busy" :aria-label="t('admin.retryFilter.ruleOptions', { name: rule.name })" :aria-expanded="ruleMenu === rule.id" @click="ruleMenu = ruleMenu === rule.id ? null : rule.id"><Icon name="more" size="sm" /></button><div v-if="ruleMenu === rule.id" class="absolute right-0 top-9 z-10 w-36 rounded-lg border border-gray-200 bg-white p-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"><button class="w-full rounded px-3 py-2 text-left text-xs hover:bg-gray-100 dark:hover:bg-dark-700" :title="t('admin.retryFilter.duplicate')" @click="ruleMenu = null; duplicateRule(rule)">{{ t('admin.retryFilter.duplicate') }}</button><button class="w-full rounded px-3 py-2 text-left text-xs text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20" :title="t('common.delete')" @click="ruleMenu = null; deleteTarget = { kind: 'rule', id: rule.id, presetID: selectedID!, name: rule.name }">{{ t('common.delete') }}</button></div></div></div>
                </article>
              </div>
              <p class="mt-3 shrink-0 border-t border-gray-200 pt-3 text-xs leading-relaxed text-gray-500 dark:border-dark-700">{{ t('admin.retryFilter.dragHint') }}</p>
            </section>

            <section v-show="activeTab === 'scope'" id="retry-filter-panel-scope" role="tabpanel" aria-labelledby="retry-filter-scope-title" class="retry-panel space-y-4 px-4 py-5 sm:px-6">
              <div class="flex flex-wrap items-center justify-between gap-3"><div class="flex flex-wrap items-center gap-3"><div class="inline-flex gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-900/50" role="group" :aria-label="t('admin.retryFilter.applyScope')"><button v-for="kind in bindingKinds" :key="kind" :data-binding-kind="kind" :aria-pressed="bindingKind === kind" class="rounded-md px-3 py-1.5 text-xs" :class="bindingKind === kind ? 'bg-white font-medium shadow-sm dark:bg-dark-700' : 'text-gray-500'" @click="bindingKind = kind">{{ t(`admin.retryFilter.${kind}`) }} ({{ kind === 'accounts' ? selectedAccountIDs.size : selectedGroupIDs.size }})</button></div><button class="btn btn-secondary btn-sm" :class="isGlobalDefault ? '!border-primary-300 !bg-primary-50 !text-primary-700 dark:!border-primary-700 dark:!bg-primary-900/20 dark:!text-primary-300' : ''" :disabled="busy || !bindingsLoaded" :aria-pressed="isGlobalDefault" aria-describedby="retry-filter-default-hint" :title="t('admin.retryFilter.defaultHint')" @click="isGlobalDefault = !isGlobalDefault"><Icon :name="isGlobalDefault ? 'check' : 'plus'" size="xs" />{{ t(isGlobalDefault ? 'admin.retryFilter.defaultShort' : 'admin.retryFilter.setAsGlobalDefault') }}</button></div><form class="flex min-w-0 flex-1 gap-2 sm:max-w-xs" @submit.prevent="searchCandidates"><input v-model="bindingSearch" class="input min-w-0" :aria-label="t('admin.retryFilter.searchBinding', { kind: t(`admin.retryFilter.${bindingKind}`) })" :placeholder="t('admin.retryFilter.searchBinding', { kind: t(`admin.retryFilter.${bindingKind}`) })" /><button class="retry-icon-button" type="submit" :disabled="candidateLoading" :aria-label="t('common.search')"><Icon name="search" size="sm" /></button></form></div>
              <span id="retry-filter-default-hint" class="sr-only">{{ t('admin.retryFilter.defaultHint') }}</span>
              <div class="flex min-w-0 flex-wrap gap-2"><span v-for="item in selectedCandidateItems" :key="item.id" class="inline-flex max-w-full items-start gap-2 rounded-lg bg-primary-50 px-2.5 py-1 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"><span class="min-w-0 break-words [overflow-wrap:anywhere]">{{ item.name }}</span><button :disabled="busy || !bindingsLoaded" :aria-label="t('admin.retryFilter.removeBinding', { name: item.name })" @click="toggleCandidate(item)">×</button></span></div>
              <p v-if="bindingError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ bindingError }}</p>
              <div v-if="candidateLoading" role="status" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
              <div v-else>
                <div class="hidden grid-cols-[1.25rem_3rem_minmax(0,1fr)_5rem_5.5rem] gap-2 border-b border-gray-200 px-2 py-2 text-xs text-gray-500 sm:grid dark:border-dark-700" aria-hidden="true"><span></span><span>{{ t('admin.retryFilter.idLabel') }}</span><span>{{ t('admin.retryFilter.name') }}</span><span>{{ t('admin.retryFilter.platform') }}</span><span>{{ t('admin.retryFilter.status') }}</span></div>
                <label v-for="item in candidateItems" :key="item.id" :data-candidate-id="item.id" class="grid cursor-pointer grid-cols-[1.25rem_minmax(0,1fr)] items-center gap-2 border-b border-gray-100 px-2 py-3 text-sm last:border-b-0 hover:bg-gray-50 sm:grid-cols-[1.25rem_3rem_minmax(0,1fr)_5rem_5.5rem] dark:border-dark-700 dark:hover:bg-dark-900/30"><input type="checkbox" class="row-span-4 h-4 w-4 self-start rounded border-gray-300 text-primary-600 focus:ring-primary-500 sm:row-span-1 sm:self-center" :disabled="busy || !bindingsLoaded || (item.eligible === false && !selectedIDs.has(item.id))" :checked="selectedIDs.has(item.id)" @change="toggleCandidate(item)" /><span class="min-w-0 break-all font-mono text-xs text-gray-500">#{{ item.id }}</span><span class="col-start-2 min-w-0 break-words [overflow-wrap:anywhere] sm:col-auto">{{ item.name }}</span><span class="col-start-2 min-w-0 break-words text-xs text-gray-500 sm:col-auto">{{ item.platform || '—' }}</span><span class="col-start-2 text-xs sm:col-auto" :class="item.eligible === false ? 'text-amber-600' : 'text-emerald-600'" :title="item.eligible === false ? t('admin.retryFilter.ineligible') : undefined">{{ t(item.eligible === false ? 'admin.retryFilter.notEligible' : 'admin.retryFilter.eligible') }}</span></label>
                <p v-if="!candidateItems.length" class="py-8 text-center text-sm text-gray-500">{{ t('admin.retryFilter.noCandidates') }}</p>
                <div class="mt-3 flex items-center justify-between gap-3 text-xs text-gray-500"><span>{{ t('admin.retryFilter.selectedCount', { count: selectedIDs.size }) }}</span><span class="flex items-center gap-2"><button class="retry-icon-button" :disabled="candidatePage <= 1 || candidateLoading" :aria-label="t('pagination.previous')" @click="changeCandidatePage(-1)"><Icon name="chevronLeft" size="sm" /></button>{{ candidatePage }} / {{ candidatePages }}<button class="retry-icon-button" :disabled="candidatePage >= candidatePages || candidateLoading" :aria-label="t('pagination.next')" @click="changeCandidatePage(1)"><Icon name="chevronRight" size="sm" /></button></span></div>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-200 pt-4 dark:border-dark-700"><p class="text-xs text-gray-500">{{ bindingsDirty ? t('admin.retryFilter.unsaved') : t('admin.retryFilter.applyScopeHint') }}</p><div class="flex gap-2"><button class="btn btn-secondary btn-sm" :disabled="busy || !bindingsDirty" @click="resetBindings">{{ t('admin.retryFilter.undoChanges') }}</button><button class="btn btn-primary btn-sm" :disabled="busy || !bindingsLoaded || !bindingsDirty" @click="saveBindings">{{ t('admin.retryFilter.saveBindings') }}</button></div></div>
            </section>

            <section v-show="activeTab === 'settings'" id="retry-filter-panel-settings" role="tabpanel" aria-labelledby="retry-filter-preset-title" class="retry-panel px-4 py-5 sm:px-6">
              <form class="max-w-2xl space-y-4" novalidate @submit.prevent="savePreset"><label class="block"><span class="input-label">{{ t('admin.retryFilter.name') }}</span><input v-model="presetForm.name" class="input" maxlength="200" :disabled="busy" /></label><label class="block"><span class="input-label">{{ t('admin.retryFilter.notes') }}</span><textarea v-model="presetForm.description" rows="3" class="input" :disabled="busy"></textarea></label><div class="flex flex-wrap items-center justify-between gap-3"><span class="text-xs text-gray-500">ID #{{ selectedPreset.id }}</span><div class="flex gap-2"><button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !presetDirty" @click="resetPresetForm">{{ t('admin.retryFilter.undoChanges') }}</button><button type="submit" class="btn btn-primary btn-sm" :disabled="busy || !presetDirty">{{ t('admin.retryFilter.savePreset') }}</button></div></div></form>
              <div class="mt-7 flex items-center justify-between gap-4 border-t border-gray-200 pt-5 dark:border-dark-700"><div><h3 class="text-sm font-medium">{{ t('admin.retryFilter.duplicatePreset') }}</h3><p class="mt-1 text-xs text-gray-500">{{ t('admin.retryFilter.duplicateHint') }}</p></div><button class="btn btn-secondary btn-sm" :disabled="busy" @click="requestDuplicatePreset">{{ t('admin.retryFilter.duplicate') }}</button></div>
              <div class="mt-5 flex items-center justify-between gap-4 border-t border-gray-200 pt-5 dark:border-dark-700"><div><h3 class="text-sm font-medium">{{ t('admin.retryFilter.deletePreset') }}</h3><p class="mt-1 text-xs text-gray-500">{{ t('admin.retryFilter.deleteHint') }}</p></div><button class="btn btn-secondary btn-sm text-red-600" :disabled="busy || !bindingsLoaded || presetInUse" :title="presetInUse ? t('admin.retryFilter.inUse') : undefined" @click="deleteTarget = { kind: 'preset', id: selectedPreset.id, name: selectedPreset.name }">{{ t('common.delete') }}</button></div>
            </section>
          </template>
        </div>
        <section v-else class="p-12 text-center text-sm text-gray-500">{{ t('admin.retryFilter.selectPreset') }}</section>
      </div>
    </div>

    <BaseDialog :show="newPresetOpen" :title="t('admin.retryFilter.addRuleSet')" :close-on-escape="!busy" @close="!busy && (newPresetOpen = false)"><form id="retry-new-preset" class="space-y-4" @submit.prevent="createPreset"><p v-if="newPresetError" role="alert" class="text-sm text-red-600">{{ newPresetError }}</p><label class="block"><span class="input-label">{{ t('admin.retryFilter.name') }}</span><input v-model="newPresetForm.name" class="input" required maxlength="200" :disabled="busy" /></label><label class="block"><span class="input-label">{{ t('admin.retryFilter.notes') }}</span><textarea v-model="newPresetForm.description" class="input" rows="2" :disabled="busy"></textarea></label></form><template #footer><button class="btn btn-secondary" :disabled="busy" @click="newPresetOpen = false">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="submit" form="retry-new-preset" :disabled="busy">{{ t('common.add') }}</button></template></BaseDialog>
    <BaseDialog :show="!!deleteTarget" :title="t('common.delete')" :close-on-escape="!busy" @close="!busy && (deleteTarget = null)"><p>{{ t('admin.retryFilter.deleteConfirm', { name: deleteTarget?.name }) }}</p><p v-if="deleteTarget?.kind === 'preset'" class="mt-2 text-sm text-gray-500">{{ t('admin.retryFilter.deleteHint') }}</p><p v-if="dialogError" role="alert" class="mt-2 text-sm text-red-600">{{ dialogError }}</p><template #footer><button class="btn btn-secondary" :disabled="busy" @click="deleteTarget = null">{{ t('common.cancel') }}</button><button class="btn btn-danger" :disabled="busy" @click="confirmDelete">{{ t('common.delete') }}</button></template></BaseDialog>
    <BaseDialog :show="editingRule !== null" :title="editingRule === 'new' ? t('admin.retryFilter.addRule') : t('admin.retryFilter.editRule')" width="wide" :close-on-escape="!busy" @close="cancelRule">
      <RetryFilterRuleEditor v-if="editingRule !== null" :key="editingRule" v-model:draft="ruleForm" :step="ruleStep" :error="dialogError" :status-error="ruleStatusError" :busy="busy" @step="setRuleStep" @submit="submitRuleStep" />
      <template #footer><div class="flex w-full flex-wrap items-center justify-between gap-3"><button class="btn btn-secondary" :disabled="busy" @click="cancelRule">{{ t('common.cancel') }}</button><div class="flex gap-2"><button v-if="ruleStep === 'action'" class="btn btn-secondary" :disabled="busy" @click="setRuleStep('conditions')">{{ t('admin.retryFilter.previousStep') }}</button><button class="btn btn-primary" type="submit" form="retry-filter-rule-editor" :disabled="busy">{{ t(ruleStep === 'conditions' ? 'admin.retryFilter.nextAction' : 'common.save') }}</button></div></div></template>
    </BaseDialog>
    <BaseDialog :show="!!pendingAction" :title="t('admin.retryFilter.discardTitle')" @close="pendingAction = null"><p class="text-sm text-gray-500">{{ t('admin.retryFilter.discardHint') }}</p><template #footer><button class="btn btn-secondary" @click="pendingAction = null">{{ t('admin.retryFilter.keepEditing') }}</button><button class="btn btn-danger" @click="discardChanges">{{ t('admin.retryFilter.discardChanges') }}</button></template></BaseDialog>
    <BaseDialog :show="replaceBindingsOpen" :title="t('admin.retryFilter.replaceBindingsTitle')" :close-on-escape="!busy" @close="!busy && (replaceBindingsOpen = false)"><p class="text-sm leading-relaxed text-gray-500">{{ t('admin.retryFilter.replaceBindingsHint') }}</p><p v-if="bindingError" role="alert" class="mt-3 text-sm text-red-600">{{ bindingError }}</p><template #footer><button class="btn btn-secondary" :disabled="busy" @click="replaceBindingsOpen = false">{{ t('common.cancel') }}</button><button class="btn btn-primary" :disabled="busy" @click="persistBindings">{{ t('admin.retryFilter.confirmReplaceBindings') }}</button></template></BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import RetryFilterGuide from '@/components/admin/RetryFilterGuide.vue'
import RetryFilterRuleEditor, { type RuleDraft, type RuleStep } from '@/components/admin/RetryFilterRuleEditor.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import retryFilterAPI, { type RetryFilterAction, type RetryFilterBindingCandidate, type RetryFilterPreset, type RetryFilterRule, type RetryFilterRuleInput } from '@/api/admin/retryFilter'
import accountsAPI from '@/api/admin/accounts'
import groupsAPI from '@/api/admin/groups'
import { useAppStore } from '@/stores/app'

type BindingKind = 'accounts' | 'groups'
type WorkspaceTab = 'rules' | 'scope' | 'settings'

const { t } = useI18n()
const app = useAppStore()
const loading = ref(true), busy = ref(false), rulesLoading = ref(false), candidateLoading = ref(false)
const error = ref(''), dialogError = ref(''), bindingError = ref(''), ruleStatusError = ref('')
const presets = ref<RetryFilterPreset[]>([]), rules = ref<RetryFilterRule[]>([])
const selectedID = ref<number | null>(null), defaultID = ref<number | null>(null)
const selectedPreset = computed(() => presets.value.find(p => p.id === selectedID.value) || null)
const presetSearch = ref('')
const filteredPresets = computed(() => { const q = presetSearch.value.trim().toLowerCase(); return q ? presets.value.filter(p => `${p.name} ${p.description || ''}`.toLowerCase().includes(q)) : presets.value })
const presetForm = reactive({ name: '', description: '', enabled: true })
const presetDirty = computed(() => !!selectedPreset.value && (presetForm.name !== selectedPreset.value.name || presetForm.description !== (selectedPreset.value.description || '')))
const editingRule = ref<number | 'new' | null>(null), draggedIndex = ref<number | null>(null)
const ruleForm = ref<RuleDraft>(newRule())
const ruleStep = ref<RuleStep>('conditions'), ruleInitial = ref(''), ruleSearch = ref(''), sorting = ref(false), ruleMenu = ref<number | null>(null)
const ruleCounts = reactive<Record<number, number>>({})
const newPresetOpen = ref(false), newPresetError = ref('')
const newPresetForm = reactive({ name: '', description: '' })
const pendingAction = ref<(() => void) | null>(null)
const activeTab = ref<WorkspaceTab>('rules')
const tabs: { key: WorkspaceTab; label: string; titleID: string }[] = [
  { key: 'rules', label: 'rulesTitle', titleID: 'retry-filter-rules-title' },
  { key: 'scope', label: 'applyScope', titleID: 'retry-filter-scope-title' },
  { key: 'settings', label: 'basicSettings', titleID: 'retry-filter-preset-title' }
]
const actionClasses: Record<RetryFilterAction, string> = {
  same_account_retry: 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300',
  switch_account: 'bg-purple-50 text-purple-700 dark:bg-purple-900/20 dark:text-purple-300',
  terminate: 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300',
  observe: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}
const filteredRules = computed(() => { const q = ruleSearch.value.trim().toLowerCase(); return rules.value.filter(r => !q || `${r.name} ${ruleSummary(r).join(' ')} ${t(`admin.retryFilter.actions.${r.action}`)}`.toLowerCase().includes(q)) })
const deleteTarget = ref<{ kind: 'preset' | 'rule'; id: number; presetID?: number; name: string } | null>(null)
const bindingKinds: BindingKind[] = ['accounts', 'groups']
const bindingKind = ref<BindingKind>('groups'), bindingSearch = ref(''), candidatePage = ref(1), candidatePages = ref(1), candidateItems = ref<RetryFilterBindingCandidate[]>([])
const selectedAccountIDs = ref<Set<number>>(new Set()), selectedGroupIDs = ref<Set<number>>(new Set()), candidateCache = reactive<Record<BindingKind, Map<number, RetryFilterBindingCandidate>>>({ accounts: new Map(), groups: new Map() })
const initialAccountIDs = ref<Set<number>>(new Set()), initialGroupIDs = ref<Set<number>>(new Set())
const isGlobalDefault = ref(false)
const bindingsLoaded = ref(false)
const replaceBindingsOpen = ref(false)
const sameIDs = (a: Set<number>, b: Set<number>) => a.size === b.size && [...a].every(id => b.has(id))
const bindingsDirty = computed(() => bindingsLoaded.value && (!sameIDs(selectedAccountIDs.value, initialAccountIDs.value) || !sameIDs(selectedGroupIDs.value, initialGroupIDs.value) || isGlobalDefault.value !== (defaultID.value === selectedID.value)))
const presetInUse = computed(() => initialAccountIDs.value.size > 0 || initialGroupIDs.value.size > 0 || defaultID.value === selectedID.value)
const ruleDirty = computed(() => editingRule.value !== null && JSON.stringify(ruleForm.value) !== ruleInitial.value)
const selectedIDs = computed(() => bindingKind.value === 'accounts' ? selectedAccountIDs.value : selectedGroupIDs.value)
const selectedCandidateItems = computed(() => Array.from(selectedIDs.value).map(id => candidateCache[bindingKind.value].get(id) || { id, name: `#${id}` }))

function newRule(): RuleDraft { return { name: '', description: '', enabled: true, priority: 0, match_mode: 'any', action: 'same_account_retry', statusText: '', keywordsText: '', codesText: '', pathsText: '', modelsText: '', retryCount: '', retryDelay: '', terminateStatus: '', terminateMessage: '', emptyRequestMode: 'none', emptyResponseMode: 'none' } }
const lines = (value: string) => value.split(/\r?\n/).map(v => v.trim()).filter(Boolean)
function parseStatusCodes(value: string): number[] | null {
  if (!value.trim()) return []
  const codes = new Set<number>()
  // Keep comma-separated entries intact so missing or malformed entries cannot be discarded.
  const entries = value.trim().replace(/\s*-\s*/g, '-').split(',')
  for (const entry of entries) {
    for (const token of entry.trim().split(/\s+/)) {
      const match = /^([1-5][0-9]{2})(?:-([1-5][0-9]{2}))?$/.exec(token)
      if (!match) return null
      const start = Number(match[1])
      const end = match[2] ? Number(match[2]) : start
      if (start > end) return null
      for (let code = start; code <= end; code++) codes.add(code)
    }
  }
  return Array.from(codes).sort((a, b) => a - b)
}
function formatStatusCodes(codes: number[]): string {
  const sorted = Array.from(new Set(codes)).sort((a, b) => a - b)
  const ranges: string[] = []
  for (let index = 0; index < sorted.length; index++) {
    const start = sorted[index]
    let end = start
    while (sorted[index + 1] === end + 1) end = sorted[++index]
    ranges.push(start === end ? String(start) : start + '-' + end)
  }
  return ranges.join(', ')
}
watch(() => ruleForm.value.statusText, () => { ruleStatusError.value = '' })
const failure = (e: unknown) => { const status = (e as { response?: { status?: number } })?.response?.status; return t(status === 409 ? 'admin.retryFilter.inUse' : 'admin.retryFilter.failed') }
const saved = () => app.showSuccess(t('admin.retryFilter.saved'))
watch(rules, list => { if (selectedID.value) ruleCounts[selectedID.value] = list.length })

function guardChanges(action: () => void, dirty = presetDirty.value || bindingsDirty.value) {
  if (dirty) pendingAction.value = action
  else action()
}
function discardChanges() { const action = pendingAction.value; pendingAction.value = null; action?.() }
function resetPresetForm() { const p = selectedPreset.value; if (p) Object.assign(presetForm, { name: p.name, description: p.description || '', enabled: p.enabled }) }
function resetBindings() { selectedAccountIDs.value = new Set(initialAccountIDs.value); selectedGroupIDs.value = new Set(initialGroupIDs.value); isGlobalDefault.value = defaultID.value === selectedID.value }
function requestRefresh() { guardChanges(() => { void load() }) }
function requestNewPreset() { guardChanges(() => { resetPresetForm(); resetBindings(); Object.assign(newPresetForm, { name: '', description: '' }); newPresetError.value = ''; newPresetOpen.value = true }) }
function requestDuplicatePreset() { guardChanges(() => { void duplicatePreset() }) }
async function navigateTabs(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const index = tabs.findIndex(tab => tab.key === activeTab.value)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length
  activeTab.value = tabs[next].key
  await nextTick()
  document.getElementById(tabs[next].titleID)?.focus()
}
watch(activeTab, () => { ruleMenu.value = null })

async function load() {
  loading.value = true; error.value = ''
  try { const [list, defaults] = await Promise.all([retryFilterAPI.listPresets(), retryFilterAPI.getDefault()]); presets.value = list; defaultID.value = defaults.preset_id || null; if (!list.some(p => p.id === selectedID.value)) selectedID.value = list[0]?.id ?? null; await loadSelected() } catch (e) { error.value = failure(e) } finally { loading.value = false }
}
function selectPreset(id: number) { if (id === selectedID.value || busy.value || rulesLoading.value) return; guardChanges(() => { selectedID.value = id; void loadSelected() }) }
async function loadSelected() {
  const id = selectedID.value
  rules.value = []; bindingsLoaded.value = false; ruleMenu.value = null; ruleSearch.value = ''; sorting.value = false
  selectedAccountIDs.value = new Set(); selectedGroupIDs.value = new Set(); initialAccountIDs.value = new Set(); initialGroupIDs.value = new Set()
  candidateCache.accounts.clear(); candidateCache.groups.clear(); candidateItems.value = []; candidatePage.value = 1; bindingSearch.value = ''
  if (!id) return
  resetPresetForm(); editingRule.value = null; rulesLoading.value = true; error.value = ''
  try { rules.value = await retryFilterAPI.listRules(id); await loadPresetBindings(id) }
  catch (e) { error.value = failure(e) }
  finally { rulesLoading.value = false }
}
watch(bindingKind, () => { candidatePage.value = 1; void loadCandidates() })
async function createPreset() {
  if (busy.value) return
  if (!newPresetForm.name.trim()) { newPresetError.value = t('admin.retryFilter.nameRequired'); return }
  busy.value = true; newPresetError.value = ''
  try { const p = await retryFilterAPI.createPreset({ name: newPresetForm.name.trim(), description: newPresetForm.description, enabled: true }); presets.value.push(p); selectedID.value = p.id; presetSearch.value = ''; activeTab.value = 'rules'; newPresetOpen.value = false; await loadSelected(); saved() }
  catch (e) { newPresetError.value = failure(e) }
  finally { busy.value = false }
}
async function savePreset() { if (busy.value) return; if (!selectedID.value || !presetForm.name.trim()) { error.value = t('admin.retryFilter.nameRequired'); return } busy.value = true; error.value = ''; try { const p = await retryFilterAPI.updatePreset(selectedID.value, { name: presetForm.name.trim(), description: presetForm.description, enabled: selectedPreset.value!.enabled }); const index = presets.value.findIndex(x => x.id === p.id); if (index >= 0) presets.value[index] = p; resetPresetForm(); saved() } catch (e) { error.value = failure(e) } finally { busy.value = false } }
async function togglePreset(enabled: boolean) {
  const p = selectedPreset.value
  if (!p || busy.value) return
  busy.value = true; error.value = ''
  try { const updated = await retryFilterAPI.updatePreset(p.id, { name: p.name, description: p.description, enabled }); presets.value[presets.value.findIndex(item => item.id === p.id)] = updated; presetForm.enabled = enabled; saved() }
  catch (e) { error.value = failure(e) }
  finally { busy.value = false }
}
async function duplicatePreset() { const source = selectedPreset.value; if (!source) return; busy.value = true; try { const copy = await retryFilterAPI.createPreset({ name: `${source.name} (${t('admin.retryFilter.copySuffix')})`, description: source.description || '', enabled: source.enabled }); const sourceRules = await retryFilterAPI.listRules(source.id); for (const rule of sourceRules) await retryFilterAPI.createRule(copy.id, ruleToPayload(rule)); presets.value.push(copy); selectedID.value = copy.id; await loadSelected(); saved() } catch (e) { error.value = failure(e) } finally { busy.value = false } }

function beginRule(rule?: RetryFilterRule) { Object.assign(ruleForm.value, newRule(), rule ? draftFromRule(rule) : { priority: rules.value.length ? Math.max(...rules.value.map(r => r.priority)) + 1 : 0 }); ruleInitial.value = JSON.stringify(ruleForm.value); ruleStep.value = 'conditions'; editingRule.value = rule?.id ?? 'new'; dialogError.value = ''; ruleStatusError.value = ''; ruleMenu.value = null }
function cancelRule() { if (busy.value) return; guardChanges(() => { editingRule.value = null; dialogError.value = ''; ruleStatusError.value = '' }, ruleDirty.value) }
function draftFromRule(rule: RetryFilterRule): RuleDraft { return { ...newRule(), name: rule.name, description: rule.description || '', enabled: rule.enabled, priority: rule.priority, match_mode: rule.match_mode, action: rule.action, statusText: formatStatusCodes(rule.status_codes || []), keywordsText: (rule.keywords || []).join('\n'), codesText: (rule.upstream_codes || []).join('\n'), pathsText: (rule.upstream_code_paths || []).join('\n'), modelsText: (rule.models || []).join('\n'), retryCount: rule.retry_count_override == null ? '' : String(rule.retry_count_override), retryDelay: rule.retry_delay_ms == null ? '' : String(rule.retry_delay_ms), terminateStatus: rule.terminate_status_code == null ? '' : String(rule.terminate_status_code), terminateMessage: rule.terminate_message || '', emptyRequestMode: rule.empty_request_mode || 'none', emptyResponseMode: rule.empty_response_mode || 'none' } }
function ruleToPayload(rule: RetryFilterRule): RetryFilterRuleInput { return { name: rule.name, description: rule.description || '', enabled: rule.enabled, priority: rule.priority, match_mode: rule.match_mode, action: rule.action, status_codes: rule.status_codes || [], keywords: rule.keywords || [], upstream_codes: rule.upstream_codes || [], upstream_code_paths: rule.upstream_code_paths || [], models: rule.models || [], retry_count_override: rule.retry_count_override ?? null, retry_delay_ms: rule.retry_delay_ms ?? null, empty_request_mode: rule.empty_request_mode ?? 'none', empty_response_mode: rule.empty_response_mode ?? 'none', terminate_status_code: rule.terminate_status_code ?? null, terminate_message: rule.terminate_message ?? null } as RetryFilterRuleInput }
function ruleConditions() {
  dialogError.value = ''
  ruleStatusError.value = ''
  const status_codes = parseStatusCodes(ruleForm.value.statusText)
  if (status_codes === null) {
    ruleStatusError.value = t('admin.retryFilter.invalidStatusCodes')
    return
  }
  const keywords = lines(ruleForm.value.keywordsText), upstream_codes = lines(ruleForm.value.codesText)
  const models = lines(ruleForm.value.modelsText), upstream_code_paths = lines(ruleForm.value.pathsText)
  if (!ruleForm.value.name.trim()) { dialogError.value = t('admin.retryFilter.nameRequired'); return }
  if (!status_codes.length && !keywords.length && !upstream_codes.length && !models.length && ruleForm.value.emptyRequestMode === 'none' && ruleForm.value.emptyResponseMode === 'none') {
    dialogError.value = t('admin.retryFilter.conditionRequired')
    return
  }
  if (models.some(m => m.includes('*') && (m === '*' || !m.endsWith('*') || m.slice(0, -1).includes('*')))) {
    dialogError.value = t('admin.retryFilter.invalidConditions')
    return
  }
  return { status_codes, keywords, upstream_codes, upstream_code_paths, models }
}
function setRuleStep(step: RuleStep) {
  if (busy.value) return
  if (step === 'conditions' || ruleConditions()) { ruleStep.value = step; dialogError.value = '' }
}
function submitRuleStep() { if (ruleStep.value === 'conditions') setRuleStep('action'); else void saveRule() }
async function saveRule() {
  if (!selectedID.value || busy.value) return
  const conditions = ruleConditions()
  if (!conditions) { ruleStep.value = 'conditions'; return }
  const retryCount = ruleForm.value.action === 'same_account_retry' && ruleForm.value.retryCount !== '' ? Number(ruleForm.value.retryCount) : null
  const retryDelay = ruleForm.value.action === 'same_account_retry' && ruleForm.value.retryDelay !== '' ? Number(ruleForm.value.retryDelay) : null
  if (retryCount != null && (!Number.isInteger(retryCount) || retryCount < 0 || retryCount > 50) || retryDelay != null && (!Number.isInteger(retryDelay) || retryDelay < 0 || retryDelay > 60000)) {
    dialogError.value = t('admin.retryFilter.invalidRetrySettings')
    return
  }
  const terminateStatus = ruleForm.value.action === 'terminate' && ruleForm.value.terminateStatus !== '' ? Number(ruleForm.value.terminateStatus) : null
  if (terminateStatus != null && (!Number.isInteger(terminateStatus) || terminateStatus < 100 || terminateStatus > 599)) {
    dialogError.value = t('admin.retryFilter.invalidTerminateStatus'); return
  }
  if (!Number.isInteger(ruleForm.value.priority)) { dialogError.value = t('admin.retryFilter.invalidPriority'); return }
  const payload: RetryFilterRuleInput = {
    name: ruleForm.value.name.trim(), description: ruleForm.value.description, enabled: ruleForm.value.enabled,
    priority: ruleForm.value.priority, match_mode: ruleForm.value.match_mode, action: ruleForm.value.action,
    ...conditions,
    retry_count_override: ruleForm.value.action === 'same_account_retry' ? retryCount : null,
    retry_delay_ms: ruleForm.value.action === 'same_account_retry' ? retryDelay : null,
    empty_request_mode: ruleForm.value.emptyRequestMode, empty_response_mode: ruleForm.value.emptyResponseMode,
    terminate_status_code: terminateStatus,
    terminate_message: ruleForm.value.action === 'terminate' && ruleForm.value.terminateMessage ? ruleForm.value.terminateMessage : null
  }
  busy.value = true
  try {
    if (editingRule.value === 'new') await retryFilterAPI.createRule(selectedID.value, payload)
    else await retryFilterAPI.updateRule(selectedID.value, Number(editingRule.value), payload)
    editingRule.value = null
    rules.value = await retryFilterAPI.listRules(selectedID.value)
    saved()
  } catch (e) { if (editingRule.value === null) error.value = failure(e); else dialogError.value = failure(e) } finally { busy.value = false }
}
async function toggleRule(rule: RetryFilterRule) { busy.value = true; try { await retryFilterAPI.updateRule(rule.preset_id, rule.id, { ...ruleToPayload(rule), enabled: !rule.enabled }); rules.value = await retryFilterAPI.listRules(rule.preset_id) } catch (e) { error.value = failure(e) } finally { busy.value = false } }
async function duplicateRule(rule: RetryFilterRule) { if (!selectedID.value) return; busy.value = true; try { await retryFilterAPI.createRule(selectedID.value, { ...ruleToPayload(rule), name: `${rule.name} (${t('admin.retryFilter.copySuffix')})`, priority: rule.priority + 1 }); rules.value = await retryFilterAPI.listRules(selectedID.value); saved() } catch (e) { error.value = failure(e) } finally { busy.value = false } }
function toggleSorting() { sorting.value = !sorting.value; ruleSearch.value = ''; ruleMenu.value = null }
function startDrag(index: number) { if (sorting.value && !busy.value) draggedIndex.value = index }
function moveRule(rule: RetryFilterRule, delta: number) { const index = rules.value.indexOf(rule); startDrag(index); void dropRule(index + delta) }
async function dropRule(index: number) {
  const from = draggedIndex.value; draggedIndex.value = null
  if (!sorting.value || busy.value || from == null || from === index || !selectedID.value || index < 0 || index >= rules.value.length) return
  const previous = rules.value
  const next = rules.value.map(r => ({ ...r })), [moved] = next.splice(from, 1)
  next.splice(index, 0, moved); next.forEach((r, i) => { r.priority = i }); rules.value = next; busy.value = true; error.value = ''
  try { await retryFilterAPI.reorderRules(selectedID.value, next.map(r => r.id)) }
  catch {
    try { for (const [i, r] of next.entries()) await retryFilterAPI.updateRule(selectedID.value, r.id, { ...ruleToPayload(r), priority: i }) }
    catch (e) { error.value = failure(e); try { rules.value = await retryFilterAPI.listRules(selectedID.value) } catch { rules.value = previous } }
  } finally { busy.value = false }
}
function ruleSummary(rule: RetryFilterRule) {
  return [
    rule.status_codes?.length ? 'HTTP ' + formatStatusCodes(rule.status_codes) : '',
    rule.keywords?.length ? t('admin.retryFilter.keywords') + ': ' + rule.keywords.join(', ') : '',
    rule.upstream_codes?.length ? t('admin.retryFilter.upstreamCodes') + ': ' + rule.upstream_codes.join(', ') : '',
    rule.models?.length ? t('admin.retryFilter.models') + ': ' + rule.models.join(', ') : '',
    rule.empty_request_mode && rule.empty_request_mode !== 'none' ? t('admin.retryFilter.emptyRequest') : '',
    rule.empty_response_mode && rule.empty_response_mode !== 'none' ? t('admin.retryFilter.emptyResponse') : ''
  ].filter(Boolean)
}

async function loadPresetBindings(id: number) {
  isGlobalDefault.value = defaultID.value === id; bindingError.value = ''; bindingsLoaded.value = false
  try {
    const result = await retryFilterAPI.getPresetBindings(id)
    selectedAccountIDs.value = new Set(result.accounts || []); selectedGroupIDs.value = new Set(result.groups || [])
    initialAccountIDs.value = new Set(selectedAccountIDs.value); initialGroupIDs.value = new Set(selectedGroupIDs.value)
    bindingsLoaded.value = true
    await loadCandidates()
  } catch (e) { bindingError.value = failure(e) }
}
let candidateRequest = 0
async function loadCandidates() {
  const request = ++candidateRequest, kind = bindingKind.value, id = selectedID.value
  const page = candidatePage.value, query = bindingSearch.value || undefined
  candidateLoading.value = true
  if (bindingsLoaded.value) bindingError.value = ''
  const current = () => request === candidateRequest && id === selectedID.value && kind === bindingKind.value
  try {
    let items: RetryFilterBindingCandidate[], pages: number
    try {
      const result = await retryFilterAPI.listBindingCandidates(kind, { q: query, page, page_size: 12, preset_id: id || undefined })
      // The candidate endpoint omits preset_id for unbound resources; legacy list fallbacks do not report it.
      items = (result.items || []).map(item => ({ ...item, preset_id: item.preset_id || null })); pages = Math.max(1, result.pages || Math.ceil((result.total || 0) / (result.page_size || 12)))
    } catch {
      if (kind === 'groups') { const result = await groupsAPI.list(page, 12, { search: query }); items = result.items.map(g => ({ id: g.id, name: g.name, platform: g.platform, eligible: true })); pages = Math.max(1, result.pages) }
      else { const result = await accountsAPI.list(page, 12, { search: query, lite: 'true', sort_by: 'id', sort_order: 'asc' }); items = result.items.map(a => ({ id: a.id, name: a.name, platform: a.platform, eligible: true })); pages = Math.max(1, result.pages) }
    }
    if (!current()) return
    candidateItems.value = items; candidatePages.value = pages
    for (const item of items) candidateCache[kind].set(item.id, item)
  } catch (e) { if (current()) { candidateItems.value = []; bindingError.value = failure(e) } }
  finally { if (request === candidateRequest) candidateLoading.value = false }
}
function searchCandidates() { candidatePage.value = 1; void loadCandidates() }
function changeCandidatePage(delta: number) { candidatePage.value = Math.min(Math.max(1, candidatePage.value + delta), candidatePages.value); void loadCandidates() }
function toggleCandidate(item: RetryFilterBindingCandidate) { if (busy.value || !bindingsLoaded.value) return; const target = bindingKind.value === 'accounts' ? selectedAccountIDs : selectedGroupIDs; const next = new Set(target.value); if (item.eligible === false && !next.has(item.id)) return; next.has(item.id) ? next.delete(item.id) : next.add(item.id); target.value = next; candidateCache[bindingKind.value].set(item.id, item) }
function saveBindings() {
  if (!selectedID.value || busy.value || !bindingsLoaded.value || !bindingsDirty.value) return
  const replacesDefault = isGlobalDefault.value && defaultID.value !== null && defaultID.value !== selectedID.value
  const replacesBinding = bindingKinds.some(kind => {
    const ids = kind === 'accounts' ? selectedAccountIDs.value : selectedGroupIDs.value
    const initial = kind === 'accounts' ? initialAccountIDs.value : initialGroupIDs.value
    return [...ids].some(id => {
      const item = candidateCache[kind].get(id)
      return !initial.has(id) && (!item || item.preset_id === undefined || (item.preset_id !== null && item.preset_id !== selectedID.value))
    })
  })
  if (replacesDefault || replacesBinding) { bindingError.value = ''; replaceBindingsOpen.value = true }
  else void persistBindings()
}
async function persistBindings() {
  if (!selectedID.value || busy.value || !bindingsLoaded.value || !bindingsDirty.value) return
  busy.value = true; bindingError.value = ''
  try {
    await retryFilterAPI.setPresetBindings(selectedID.value, { accounts: Array.from(selectedAccountIDs.value), groups: Array.from(selectedGroupIDs.value), global: isGlobalDefault.value })
    defaultID.value = isGlobalDefault.value ? selectedID.value : (defaultID.value === selectedID.value ? null : defaultID.value)
    for (const kind of bindingKinds) {
      const ids = kind === 'accounts' ? selectedAccountIDs.value : selectedGroupIDs.value
      for (const [id, item] of candidateCache[kind]) {
        if (ids.has(id)) item.preset_id = selectedID.value
        else if (item.preset_id === selectedID.value) item.preset_id = null
      }
    }
    initialAccountIDs.value = new Set(selectedAccountIDs.value); initialGroupIDs.value = new Set(selectedGroupIDs.value)
    replaceBindingsOpen.value = false; saved()
  } catch (e) { bindingError.value = failure(e) }
  finally { busy.value = false }
}
async function confirmDelete() { const target = deleteTarget.value; if (!target) return; busy.value = true; dialogError.value = ''; try { if (target.kind === 'preset') { await retryFilterAPI.deletePreset(target.id); presets.value = presets.value.filter(p => p.id !== target.id); selectedID.value = presets.value[0]?.id ?? null; if (selectedID.value) await loadSelected() } else { await retryFilterAPI.deleteRule(target.presetID!, target.id); rules.value = rules.value.filter(r => r.id !== target.id) } deleteTarget.value = null; saved() } catch (e) { dialogError.value = failure(e) } finally { busy.value = false } }
function closeRuleMenu(event: MouseEvent | KeyboardEvent) {
  if (event instanceof KeyboardEvent ? event.key === 'Escape' : !(event.target instanceof Element && event.target.closest('.retry-rule-tools'))) ruleMenu.value = null
}
onMounted(() => { void load(); document.addEventListener('click', closeRuleMenu); document.addEventListener('keydown', closeRuleMenu) })
onBeforeUnmount(() => { candidateRequest++; document.removeEventListener('click', closeRuleMenu); document.removeEventListener('keydown', closeRuleMenu) })

</script>

<style scoped>
.retry-workspace { display: grid; grid-template-columns: minmax(0, 1fr); }
.retry-presets-list { max-height: 200px; overflow: auto; }
.retry-rule-row { display: grid; grid-template-columns: 26px minmax(0, 1fr) 154px 44px 70px; gap: 14px; align-items: center; }
.retry-icon-button { display: inline-flex; flex: none; width: 32px; height: 32px; align-items: center; justify-content: center; border-radius: 8px; }
.retry-icon-button:hover { background: rgb(148 163 184 / 12%); }
.retry-icon-button:focus-visible { outline: 2px solid #0d9488; outline-offset: 2px; }
.retry-icon-button:disabled { opacity: .4; cursor: not-allowed; }
@media (min-width: 1024px) {
  .retry-workspace { grid-template-columns: 224px minmax(0, 1fr); min-height: 520px; height: calc(100dvh - 245px); max-height: 850px; }
  .retry-presets { display: flex; flex-direction: column; min-height: 0; }
  .retry-presets-list { flex: 1; max-height: none; min-height: 0; }
  .retry-main { display: flex; flex-direction: column; min-height: 0; }
  .retry-panel { flex: 1; min-height: 0; overflow: auto; }
  .retry-rules { display: flex; flex-direction: column; overflow: hidden; }
  .retry-rule-list { min-height: 0; flex: 1; overflow: auto; padding-bottom: 70px; }
  .retry-rule-head { flex-shrink: 0; }
}
@media (min-width: 1536px) { .retry-rule-row { grid-template-columns: 30px minmax(0, 1fr) 170px 100px 70px; } }
@media (max-width: 1279px) { .retry-rule-row { grid-template-columns: 24px minmax(0, 1fr) 126px 36px 68px; gap: 10px; } }
@media (max-width: 639px) {
  .retry-rule-row { grid-template-columns: 22px minmax(0, 1fr) 68px; gap: 12px 8px; }
  .retry-rule-head { display: none; }
  .retry-order { grid-column: 1; grid-row: 1 / 3; align-self: start; }
  .retry-rule-info { grid-column: 2; grid-row: 1; }
  .retry-action { grid-column: 2; grid-row: 2; }
  .retry-status { grid-column: 3; grid-row: 2; justify-content: end; padding-right: 6px; }
  .retry-rule-tools { grid-column: 3; grid-row: 1; align-self: start; }
}
</style>
