import { describe, expect, it } from 'vitest'
import type { TopSqlEntry } from './topSql'
import { elapsedLabel, statementSummary, statementText, topSqlRowKey } from './topSql'

function entry(overrides: Partial<TopSqlEntry> = {}): TopSqlEntry {
  return {
    instance_id: '11111111-1111-4111-8111-111111111111',
    instance_name: '订单库主库',
    queryid: '-4029384029384',
    calls: 12,
    total_exec_time_ms: 1234,
    ...overrides,
  }
}

describe('top SQL projection', () => {
  it('shows the normalised statement text rather than the identifier', () => {
    expect(statementSummary(entry({ query_text: 'SELECT * FROM orders WHERE id = $1' })))
      .toBe('SELECT * FROM orders WHERE id = $1')
  })

  it('collapses a multi-line statement into one line for the cell', () => {
    expect(statementSummary(entry({ query_text: 'SELECT *\n  FROM orders\n  WHERE id = $1' })))
      .toBe('SELECT * FROM orders WHERE id = $1')
  })

  it('says the text was not captured instead of rendering a blank row', () => {
    expect(statementSummary(entry())).toBe('queryid -4029384029384（未采到 SQL 文本）')
    expect(statementSummary(entry({ query_text: '' }))).toBe('queryid -4029384029384（未采到 SQL 文本）')
  })

  it('reports missing text as absent so the detail view can explain instead of drawing an empty block', () => {
    expect(statementText(entry())).toBeUndefined()
    expect(statementText(entry({ query_text: '' }))).toBeUndefined()
    expect(statementText(entry({ query_text: 'SELECT 1' }))).toBe('SELECT 1')
  })

  it('keys a row by instance and queryid, which is exactly the text dedup key', () => {
    expect(topSqlRowKey(entry())).toBe('11111111-1111-4111-8111-111111111111:-4029384029384')
  })

  it('steps the elapsed-time unit up so the magnitude stays readable', () => {
    expect(elapsedLabel(12.34)).toBe('12.3 ms')
    expect(elapsedLabel(999)).toBe('999.0 ms')
    expect(elapsedLabel(1500)).toBe('1.5 s')
    expect(elapsedLabel(90_000)).toBe('1.5 min')
    expect(elapsedLabel(5_400_000)).toBe('1.5 h')
  })
})
