<template>
  <BaseDialog
    :show="show"
    :title="t('modelPricing.help.title')"
    width="wide"
    @close="emit('close')"
  >
    <div data-test="pricing-help-dialog" class="text-sm text-gray-600 dark:text-gray-300">
      <p class="border-b border-gray-200 pb-5 leading-6 dark:border-dark-700">
        {{ t('modelPricing.help.intro') }}
      </p>

      <section class="border-b border-gray-200 py-5 dark:border-dark-700">
        <h4 class="flex items-center gap-2 font-semibold text-gray-900 dark:text-white">
          <Icon name="calculator" size="sm" class="text-primary-500" />
          {{ t('modelPricing.help.priceModes.title') }}
        </h4>
        <dl class="mt-4 grid gap-4 sm:grid-cols-[9rem_1fr]">
          <dt class="font-medium text-gray-900 dark:text-gray-100">
            {{ t('modelPricing.help.priceModes.rawTitle') }}
          </dt>
          <dd class="leading-6">{{ t('modelPricing.help.priceModes.rawDescription') }}</dd>
          <dt class="font-medium text-gray-900 dark:text-gray-100">
            {{ t('modelPricing.help.priceModes.actualTitle') }}
          </dt>
          <dd class="leading-6">{{ t('modelPricing.help.priceModes.actualDescription') }}</dd>
        </dl>
        <div class="mt-4 border-l-2 border-primary-500 bg-primary-50/70 px-4 py-3 dark:bg-primary-950/20">
          <div class="font-mono text-xs font-semibold text-primary-800 dark:text-primary-300">
            {{ t('modelPricing.help.priceModes.formula') }}
          </div>
          <p class="mt-2 leading-6 text-gray-600 dark:text-gray-300">
            {{ t('modelPricing.help.priceModes.example') }}
          </p>
        </div>
        <p class="mt-4 leading-6">{{ t('modelPricing.help.priceModes.unitPriceNote') }}</p>
        <p class="mt-2 leading-6">{{ t('modelPricing.help.priceModes.snapshotNote') }}</p>
      </section>

      <section class="border-b border-gray-200 py-5 dark:border-dark-700">
        <h4 class="font-semibold text-gray-900 dark:text-white">
          {{ t('modelPricing.help.multipliers.title') }}
        </h4>
        <dl class="mt-4 grid gap-4 sm:grid-cols-[9rem_1fr]">
          <dt class="font-medium text-gray-900 dark:text-gray-100">
            {{ t('modelPricing.help.multipliers.groupTitle') }}
          </dt>
          <dd class="leading-6">{{ t('modelPricing.help.multipliers.groupDescription') }}</dd>
          <dt class="font-medium text-gray-900 dark:text-gray-100">
            {{ t('modelPricing.help.multipliers.peakTitle') }}
          </dt>
          <dd class="leading-6">{{ t('modelPricing.help.multipliers.peakDescription') }}</dd>
          <dt class="font-medium text-gray-900 dark:text-gray-100">
            {{ t('modelPricing.help.multipliers.modelTitle') }}
          </dt>
          <dd class="leading-6">{{ t('modelPricing.help.multipliers.modelDescription') }}</dd>
          <dt class="font-medium text-gray-900 dark:text-gray-100">
            {{ t('modelPricing.help.multipliers.effectiveTitle') }}
          </dt>
          <dd class="leading-6">
            <p>{{ t('modelPricing.help.multipliers.effectiveDescription') }}</p>
            <p class="mt-2 font-mono text-xs font-semibold text-gray-800 dark:text-gray-200">
              {{ t('modelPricing.help.multipliers.formula') }}
            </p>
          </dd>
        </dl>
      </section>

      <section class="grid border-b border-gray-200 py-5 dark:border-dark-700 md:grid-cols-2 md:gap-8">
        <div>
          <h4 class="font-semibold text-gray-900 dark:text-white">
            {{ t('modelPricing.help.contextTiers.title') }}
          </h4>
          <p class="mt-3 leading-6">{{ t('modelPricing.help.contextTiers.description') }}</p>
          <p class="mt-2 leading-6">{{ t('modelPricing.help.contextTiers.boundary') }}</p>
          <p class="mt-2 leading-6">{{ t('modelPricing.help.contextTiers.noTier') }}</p>
        </div>
        <div class="mt-5 border-t border-gray-200 pt-5 dark:border-dark-700 md:mt-0 md:border-l md:border-t-0 md:pl-8 md:pt-0">
          <h4 class="font-semibold text-gray-900 dark:text-white">
            {{ t('modelPricing.help.timeBands.title') }}
          </h4>
          <p class="mt-3 leading-6">{{ t('modelPricing.help.timeBands.pricingDescription') }}</p>
          <p class="mt-2 leading-6">{{ t('modelPricing.help.timeBands.groupPeakDescription') }}</p>
          <p class="mt-2 leading-6">{{ t('modelPricing.help.timeBands.timezone') }}</p>
        </div>
      </section>

      <section class="grid py-5 md:grid-cols-2 md:gap-8">
        <div>
          <h4 class="font-semibold text-gray-900 dark:text-white">
            {{ t('modelPricing.help.offers.title') }}
          </h4>
          <p class="mt-3 leading-6">{{ t('modelPricing.help.offers.description') }}</p>
          <p class="mt-2 leading-6 font-medium text-gray-800 dark:text-gray-200">
            {{ t('modelPricing.help.offers.noPricingEffect') }}
          </p>
        </div>
        <div class="mt-5 border-t border-gray-200 pt-5 dark:border-dark-700 md:mt-0 md:border-l md:border-t-0 md:pl-8 md:pt-0">
          <h4 class="font-semibold text-gray-900 dark:text-white">
            {{ t('modelPricing.help.columns.title') }}
          </h4>
          <p class="mt-3 leading-6">{{ t('modelPricing.help.columns.tokenPrices') }}</p>
          <p class="mt-2 leading-6">{{ t('modelPricing.help.columns.billingMode') }}</p>
          <p class="mt-2 leading-6">{{ t('modelPricing.help.columns.source') }}</p>
        </div>
      </section>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="emit('close')">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'

defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  (event: 'close'): void
}>()

const { t } = useI18n()
</script>
