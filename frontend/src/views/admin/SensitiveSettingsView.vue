<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <!-- Loading State -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>

      <!-- Settings Form -->
      <form v-else @submit.prevent="saveSettings" class="space-y-6">
        <!-- Upstream Error Sanitization -->
        <div class="card">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('admin.sensitiveSettings.upstreamErrorSanitization.title') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.sensitiveSettings.upstreamErrorSanitization.description') }}
            </p>
          </div>
          <div class="space-y-4 p-6">
            <!-- Toggle -->
            <div class="flex items-center justify-between">
              <div>
                <label class="font-medium text-gray-900 dark:text-white">
                  {{ t('admin.sensitiveSettings.upstreamErrorSanitization.enabled') }}
                </label>
              </div>
              <Toggle v-model="form.upstreamErrorSanitizationEnabled" />
            </div>

            <!-- Warning when disabled -->
            <div
              v-if="!form.upstreamErrorSanitizationEnabled"
              class="rounded-lg border border-amber-200 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-900/20"
            >
              <div class="flex items-start">
                <Icon
                  name="exclamationTriangle"
                  size="md"
                  class="mt-0.5 flex-shrink-0 text-amber-500"
                />
                <p class="ml-3 text-sm text-amber-700 dark:text-amber-300">
                  {{ t('admin.sensitiveSettings.upstreamErrorSanitization.warning') }}
                </p>
              </div>
            </div>
          </div>
        </div>

        <!-- Save Button -->
        <div class="flex justify-end">
          <button type="submit" :disabled="saving" class="btn btn-primary">
            <svg v-if="saving" class="mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </form>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api'
import type { SystemSettings } from '@/api/admin/settings'
import { useAppStore } from '@/stores'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const saving = ref(false)

const form = reactive({
  upstreamErrorSanitizationEnabled: true
})

async function loadSettings() {
  loading.value = true
  try {
    const settings: SystemSettings = await adminAPI.settings.getSettings()
    form.upstreamErrorSanitizationEnabled = settings.upstream_error_sanitization_enabled ?? true
  } catch (error: any) {
    console.error('Failed to load settings:', error)
    appStore.showError(t('admin.sensitiveSettings.saveFailed') + ': ' + (error.message || t('common.unknownError')))
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    await adminAPI.settings.updateSettings({
      upstream_error_sanitization_enabled: form.upstreamErrorSanitizationEnabled
    })
    appStore.showSuccess(t('admin.sensitiveSettings.saved'))
  } catch (error: any) {
    console.error('Failed to save settings:', error)
    appStore.showError(t('admin.sensitiveSettings.saveFailed') + ': ' + (error.message || t('common.unknownError')))
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadSettings()
})
</script>
