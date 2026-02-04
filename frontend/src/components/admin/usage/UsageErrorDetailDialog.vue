<template>
  <BaseDialog :show="show" :title="t('admin.usage.errorDetails')" width="wide" @close="$emit('close')">
    <div v-if="log" class="space-y-6">
      <!-- Error Summary -->
      <div class="rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-800 dark:bg-red-900/20">
        <div class="flex items-start gap-3">
          <svg class="h-5 w-5 text-red-500 dark:text-red-400 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <div class="flex-1 min-w-0">
            <h4 class="text-sm font-medium text-red-800 dark:text-red-200">
              {{ log.error_type || t('admin.usage.unknownError') }}
            </h4>
            <p v-if="log.error_message" class="mt-1 text-sm text-red-700 dark:text-red-300">
              {{ log.error_message }}
            </p>
            <div class="mt-2 flex flex-wrap gap-2 text-xs">
              <span v-if="log.error_status_code" class="inline-flex items-center rounded bg-red-100 px-2 py-0.5 text-red-800 dark:bg-red-800 dark:text-red-200">
                HTTP {{ log.error_status_code }}
              </span>
              <span v-if="log.upstream_status_code" class="inline-flex items-center rounded bg-orange-100 px-2 py-0.5 text-orange-800 dark:bg-orange-800 dark:text-orange-200">
                {{ t('admin.usage.upstream') }}: {{ log.upstream_status_code }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- Request Info -->
      <div>
        <h4 class="text-sm font-medium text-gray-900 dark:text-white mb-2">{{ t('admin.usage.requestInfo') }}</h4>
        <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
          <div>
            <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.usage.requestId') }}</dt>
            <dd class="font-mono text-gray-900 dark:text-white truncate" :title="log.request_id">{{ log.request_id || '-' }}</dd>
          </div>
          <div>
            <dt class="text-gray-500 dark:text-gray-400">{{ t('usage.model') }}</dt>
            <dd class="text-gray-900 dark:text-white">{{ log.model || '-' }}</dd>
          </div>
          <div>
            <dt class="text-gray-500 dark:text-gray-400">{{ t('usage.duration') }}</dt>
            <dd class="text-gray-900 dark:text-white">{{ formatDuration(log.duration_ms) }}</dd>
          </div>
          <div>
            <dt class="text-gray-500 dark:text-gray-400">{{ t('usage.time') }}</dt>
            <dd class="text-gray-900 dark:text-white">{{ formatDateTime(log.created_at) }}</dd>
          </div>
        </dl>
      </div>

      <!-- Request Headers -->
      <div v-if="log.request_headers">
        <div class="flex items-center justify-between mb-2">
          <h4 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.usage.requestHeaders') }}</h4>
          <button type="button" @click="headersExpanded = !headersExpanded" class="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400">
            {{ headersExpanded ? t('common.collapse') : t('common.expand') }}
          </button>
        </div>
        <pre v-if="headersExpanded" class="rounded-lg bg-gray-100 p-3 text-xs font-mono text-gray-800 dark:bg-gray-800 dark:text-gray-200 overflow-x-auto max-h-48">{{ formatJSON(log.request_headers) }}</pre>
        <p v-else class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usage.headersCollapsed') }}</p>
      </div>

      <!-- Upstream Error -->
      <div v-if="log.upstream_error_message || log.upstream_errors">
        <h4 class="text-sm font-medium text-gray-900 dark:text-white mb-2">{{ t('admin.usage.upstreamError') }}</h4>
        <div class="rounded-lg border border-orange-200 bg-orange-50 p-3 dark:border-orange-800 dark:bg-orange-900/20">
          <p v-if="log.upstream_error_message" class="text-sm text-orange-800 dark:text-orange-200">
            {{ log.upstream_error_message }}
          </p>
          <div v-if="parsedUpstreamErrors.length > 0" class="mt-2 space-y-1">
            <p class="text-xs font-medium text-orange-700 dark:text-orange-300">{{ t('admin.usage.upstreamEvents') }}:</p>
            <ul class="list-disc list-inside text-xs text-orange-700 dark:text-orange-300">
              <li v-for="(err, idx) in parsedUpstreamErrors" :key="idx">{{ err }}</li>
            </ul>
          </div>
        </div>
      </div>

      <!-- Error Body -->
      <div v-if="log.error_body">
        <div class="flex items-center justify-between mb-2">
          <h4 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.usage.errorBody') }}</h4>
          <button type="button" @click="bodyExpanded = !bodyExpanded" class="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400">
            {{ bodyExpanded ? t('common.collapse') : t('common.expand') }}
          </button>
        </div>
        <pre v-if="bodyExpanded" class="rounded-lg bg-gray-100 p-3 text-xs font-mono text-gray-800 dark:bg-gray-800 dark:text-gray-200 overflow-x-auto max-h-64">{{ formatJSON(log.error_body) }}</pre>
        <p v-else class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usage.bodyCollapsed') }}</p>
      </div>
    </div>

    <template #footer>
      <button type="button" @click="$emit('close')" class="btn btn-secondary">
        {{ t('common.close') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { formatDateTime } from '@/utils/format'
import type { AdminUsageLog } from '@/types'

const props = defineProps<{
  show: boolean
  log: AdminUsageLog | null
}>()

defineEmits<{
  close: []
}>()

const { t } = useI18n()

const headersExpanded = ref(false)
const bodyExpanded = ref(false)

const parsedUpstreamErrors = computed(() => {
  if (!props.log?.upstream_errors) return []
  try {
    const parsed = JSON.parse(props.log.upstream_errors)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
})

const formatDuration = (ms: number | null | undefined): string => {
  if (ms == null) return '-'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

const formatJSON = (str: string | null | undefined): string => {
  if (!str) return ''
  try {
    const parsed = JSON.parse(str)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return str
  }
}
</script>
