import { describe, expect, it } from 'vitest'
import { alertSettingsTabLabel, alertSettingsTabs, parseAlertSettingsSearch } from './tabs'

describe('alert settings tab addressing', () => {
  it('names every tab exactly once', () => {
    expect(alertSettingsTabs.map(alertSettingsTabLabel)).toEqual(['通知渠道', '联系人', '通知策略', '维护窗口'])
  })

  it('falls back to the first tab rather than rendering nothing', () => {
    expect(parseAlertSettingsSearch({})).toEqual({ tab: 'channels' })
    expect(parseAlertSettingsSearch({ tab: 'nope' })).toEqual({ tab: 'channels' })
    expect(parseAlertSettingsSearch({ tab: 'maintenance' })).toEqual({ tab: 'maintenance' })
  })

  it('carries the maintenance shortcut through', () => {
    expect(parseAlertSettingsSearch({ tab: 'maintenance', instance_id: 'instance-1', new_window: 'true' }))
      .toEqual({ tab: 'maintenance', instance_id: 'instance-1', new_window: true })
    expect(parseAlertSettingsSearch({ tab: 'maintenance', instance_id: 42 })).toEqual({ tab: 'maintenance' })
  })
})
