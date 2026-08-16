import { describe, expect, it } from 'vitest'
import type { components } from '../../api/schema'
import { policyFormValues, policyInput, repeatIntervalUnitSeconds } from './policies'

type Policy = components['schemas']['NotificationPolicy']

describe('notification policy form mapping', () => {
  it('round-trips repeat seconds and selected channel targets', () => {
    const policy = {
      id: '00000000-0000-4000-8000-000000000081',
      name: 'Critical on-call',
      is_default: false,
      contact_ids: ['00000000-0000-4000-8000-000000000082'],
      contact_group_ids: [],
      channels: [
        { channel: 'SMTP' },
        { channel: 'WEBHOOK', target_id: '00000000-0000-4000-8000-000000000083' },
      ],
      severity_filter: ['critical'],
      notify_on_fire: true,
      notify_on_recovery: false,
      repeat_interval: 900,
      created_at: '2026-08-11T12:00:00Z',
      updated_at: '2026-08-11T12:00:00Z',
    } satisfies Policy

    const form = policyFormValues(policy, 60)
    expect(form.repeat_interval_value).toBe(15)
    expect(form.smtp_enabled).toBe(true)
    expect(form.webhook_target_ids).toEqual(['00000000-0000-4000-8000-000000000083'])
    expect(policyInput(form, 60)).toEqual({
      name: policy.name,
      contact_ids: policy.contact_ids,
      contact_group_ids: [],
      channels: policy.channels,
      severity_filter: ['critical'],
      notify_on_fire: true,
      notify_on_recovery: false,
      repeat_interval: 900,
      template_id: undefined,
    })

    const secondsForm = policyFormValues({ ...policy, repeat_interval: 30 }, 1)
    expect(secondsForm.repeat_interval_value).toBe(30)
    expect(policyInput(secondsForm, 1).repeat_interval).toBe(30)
  })

  it('selects a unit that represents the deployment minimum exactly', () => {
    expect(repeatIntervalUnitSeconds(900)).toBe(60)
    expect(repeatIntervalUnitSeconds(90)).toBe(1)
    expect(repeatIntervalUnitSeconds(30)).toBe(1)
  })
})
