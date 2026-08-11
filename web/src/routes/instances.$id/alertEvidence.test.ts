import { describe, expect, it } from 'vitest'
import { triggerSnapshotPresentation } from './alertEvidence'

describe('trigger snapshot presentation', () => {
  it.each([
    ['SUCCESS', '采集成功', 'success'],
    ['FAILED', '采集失败', 'error'],
    ['NOT_APPLICABLE', '该类型不采集现场快照', 'not-applicable'],
  ] as const)('distinguishes %s', (result, label, kind) => {
    expect(triggerSnapshotPresentation(result)).toEqual({ label, kind })
  })
})
