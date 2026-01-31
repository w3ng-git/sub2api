<template>
  <BaseDialog
    :show="show"
    :title="t('admin.users.createUser')"
    width="normal"
    @close="$emit('close')"
  >
    <form id="create-user-form" @submit.prevent="submit" class="space-y-5">
      <div>
        <label class="input-label">{{ t('admin.users.email') }}</label>
        <input v-model="form.email" type="email" required class="input" :placeholder="t('admin.users.enterEmail')" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.password') }}</label>
        <div class="flex gap-2">
          <div class="relative flex-1">
            <input v-model="form.password" type="text" required class="input pr-10" :placeholder="t('admin.users.enterPassword')" />
          </div>
          <button type="button" @click="generateRandomPassword" class="btn btn-secondary px-3">
            <Icon name="refresh" size="md" />
          </button>
        </div>
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.username') }}</label>
        <input v-model="form.username" type="text" class="input" :placeholder="t('admin.users.enterUsername')" />
      </div>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div>
          <label class="input-label">{{ t('admin.users.columns.balance') }}</label>
          <input v-model.number="form.balance" type="number" step="any" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.users.columns.concurrency') }}</label>
          <input v-model.number="form.concurrency" type="number" class="input" />
        </div>
      </div>
      <!-- 缓存转移配置（管理员可见，用户不可见） -->
      <div class="border-t pt-4">
        <p class="mb-3 text-xs font-medium uppercase tracking-wider text-gray-400">
          {{ t('admin.users.cacheTransfer.section') }}
        </p>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label class="input-label">{{ t('admin.users.cacheTransfer.ratioTitle') }}</label>
            <input v-model="form.cache_read_transfer_ratio" type="number"
              step="0.01" min="0" max="1" class="input"
              :placeholder="t('admin.users.cacheTransfer.placeholder')" />
            <p class="input-hint">{{ t('admin.users.cacheTransfer.ratioHint') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('admin.users.cacheTransfer.probabilityTitle') }}</label>
            <input v-model="form.cache_read_transfer_probability" type="number"
              step="0.01" min="0" max="1" class="input"
              :placeholder="t('admin.users.cacheTransfer.placeholder')" />
            <p class="input-hint">{{ t('admin.users.cacheTransfer.probabilityHint') }}</p>
          </div>
        </div>
      </div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" type="button" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="create-user-form" :disabled="loading" class="btn btn-primary">
          {{ loading ? t('admin.users.creating') : t('common.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'; import { adminAPI } from '@/api/admin'
import { useForm } from '@/composables/useForm'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits(['close', 'success']); const { t } = useI18n()

const form = reactive({ email: '', password: '', username: '', notes: '', balance: 0, concurrency: 1, cache_read_transfer_ratio: '' as number | string, cache_read_transfer_probability: '' as number | string })

const { loading, submit } = useForm({
  form,
  submitFn: async (data) => {
    const payload: any = { ...data }
    // 缓存转移配置：空字符串转 null（不设置，使用分组默认）
    payload.cache_read_transfer_ratio = data.cache_read_transfer_ratio !== '' ? Number(data.cache_read_transfer_ratio) : null
    payload.cache_read_transfer_probability = data.cache_read_transfer_probability !== '' ? Number(data.cache_read_transfer_probability) : null
    await adminAPI.users.create(payload)
    emit('success'); emit('close')
  },
  successMsg: t('admin.users.userCreated')
})

watch(() => props.show, (v) => { if(v) Object.assign(form, { email: '', password: '', username: '', notes: '', balance: 0, concurrency: 1, cache_read_transfer_ratio: '', cache_read_transfer_probability: '' }) })

const generateRandomPassword = () => {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*'
  let p = ''; for (let i = 0; i < 16; i++) p += chars.charAt(Math.floor(Math.random() * chars.length))
  form.password = p
}
</script>
