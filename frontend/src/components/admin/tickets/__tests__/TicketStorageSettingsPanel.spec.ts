import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TicketStorageSettingsPanel from '../TicketStorageSettingsPanel.vue'
import type { TicketStorageSettings } from '@/types/ticket'

const {
  getStorageSettings,
  testStorageSettings,
  updateStorageSettings,
  refreshNotifications,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getStorageSettings: vi.fn(),
  testStorageSettings: vi.fn(),
  updateStorageSettings: vi.fn(),
  refreshNotifications: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api', () => ({
  adminAPI: {
    tickets: {
      getStorageSettings,
      testStorageSettings,
      updateStorageSettings,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError, showSuccess }),
  useTicketNotificationsStore: () => ({ refresh: refreshNotifications }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

const settings: TicketStorageSettings = {
  mode: 's3',
  attachments_enabled: true,
  local: {
    configured: true,
    display_path: './data/ticket-attachments',
    writable: true,
    shared_volume: false,
  },
  s3: {
    endpoint: 'https://s3.example.com',
    region: 'auto',
    bucket: 'tickets',
    access_key_id_masked: 'AKIA****TEST',
    secret_configured: true,
    prefix: 'ticket-attachments/',
    force_path_style: false,
  },
  usage: {
    local: { files: 2, bytes: 2048 },
    s3: { files: 3, bytes: 4096 },
  },
}

function mountPanel() {
  return mount(TicketStorageSettingsPanel, {
    global: {
      stubs: {
        LoadingSpinner: true,
        Icon: true,
      },
    },
  })
}

async function loadedPanel() {
  const wrapper = mountPanel()
  await flushPromises()
  return wrapper
}

function buttonByText(wrapper: ReturnType<typeof mountPanel>, text: string) {
  return wrapper.findAll('button').find((button) => button.text() === text)
}

describe('TicketStorageSettingsPanel', () => {
  beforeEach(() => {
    getStorageSettings.mockReset()
    testStorageSettings.mockReset()
    updateStorageSettings.mockReset()
    refreshNotifications.mockReset()
    showError.mockReset()
    showSuccess.mockReset()

    getStorageSettings.mockResolvedValue(structuredClone(settings))
    testStorageSettings.mockResolvedValue(undefined)
    updateStorageSettings.mockResolvedValue(structuredClone(settings))
    refreshNotifications.mockResolvedValue(undefined)
  })

  it('loads masked S3 settings and sends entered credentials to the probe', async () => {
    const wrapper = await loadedPanel()
    const inputs = wrapper.findAll('input')

    expect(inputs[3].attributes('placeholder')).toBe('AKIA****TEST')
    expect(inputs[4].attributes('placeholder')).toBe('admin.tickets.storage.secretPlaceholder')

    await inputs[0].setValue('https://new-s3.example.com')
    await inputs[1].setValue('us-east-1')
    await inputs[2].setValue('new-tickets')
    await inputs[3].setValue('new-access-key')
    await inputs[4].setValue('new-secret-key')
    await inputs[5].setValue('new-prefix/')
    await inputs[6].setValue(true)

    await buttonByText(wrapper, 'admin.tickets.storage.test')?.trigger('click')
    await flushPromises()

    expect(testStorageSettings).toHaveBeenCalledWith({
      mode: 's3',
      s3: {
        endpoint: 'https://new-s3.example.com',
        region: 'us-east-1',
        bucket: 'new-tickets',
        access_key_id: 'new-access-key',
        secret_access_key: 'new-secret-key',
        prefix: 'new-prefix/',
        force_path_style: true,
      },
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.tickets.storage.testSuccess')
  })

  it('clears entered credentials and refreshes capabilities after saving', async () => {
    const wrapper = await loadedPanel()
    const inputs = wrapper.findAll('input')
    await inputs[3].setValue('rotated-access-key')
    await inputs[4].setValue('rotated-secret-key')

    await buttonByText(wrapper, 'admin.tickets.storage.save')?.trigger('click')
    await flushPromises()

    expect(updateStorageSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        mode: 's3',
        s3: expect.objectContaining({
          access_key_id: 'rotated-access-key',
          secret_access_key: 'rotated-secret-key',
        }),
      }),
    )
    expect((inputs[3].element as HTMLInputElement).value).toBe('')
    expect((inputs[4].element as HTMLInputElement).value).toBe('')
    expect(refreshNotifications).toHaveBeenCalledWith(true, true)
  })

  it('maps a destination-in-use reason to the dedicated error message', async () => {
    updateStorageSettings.mockRejectedValueOnce({
      code: 409,
      reason: 'TICKET_STORAGE_DESTINATION_IN_USE',
      message: 'conflict',
    })
    const wrapper = await loadedPanel()

    await buttonByText(wrapper, 'admin.tickets.storage.save')?.trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.tickets.storage.destinationInUse')
    expect(refreshNotifications).not.toHaveBeenCalled()
  })
})
