import { describe, expect, it } from 'vitest'
import { instanceEngineLabel, instanceEngines } from './instanceEngine'

describe('instance engine', () => {
  it('spells each engine the way its product does', () => {
    expect(instanceEngineLabel('POSTGRESQL')).toBe('PostgreSQL')
  })

  it('offers every engine the API knows about', () => {
    // 清单是接入表单的下拉项。多一个引擎时这条会先红，提醒把文案一起补上。
    expect([...instanceEngines]).toEqual(['POSTGRESQL'])
    for (const engine of instanceEngines) {
      expect(instanceEngineLabel(engine)).not.toBe('')
    }
  })
})
