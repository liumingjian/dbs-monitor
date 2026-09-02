import { describe, expect, it } from 'vitest'
import { formatSql, sqlSummary, sqlTokens } from './sqlText'

describe('sqlTokens', () => {
  it('分出关键字、标识符、参数与注释', () => {
    const kinds = sqlTokens('SELECT id FROM "user" WHERE name = $1 -- 备注')
      .filter((token) => token.kind !== 'whitespace')
      .map((token) => `${token.kind}:${token.text}`)

    expect(kinds).toEqual([
      'keyword:SELECT',
      'identifier:id',
      'keyword:FROM',
      'identifier:"user"',
      'keyword:WHERE',
      'identifier:name',
      'operator:=',
      'parameter:$1',
      'comment:-- 备注',
    ])
  })

  it('多字符运算符不被切碎', () => {
    expect(sqlTokens("a::text || b->>'k'").filter((token) => token.kind === 'operator').map((token) => token.text))
      .toEqual(['::', '||', '->>'])
  })

  it('字符串里的两个单引号是转义而不是结束', () => {
    expect(sqlTokens("'it''s' x").filter((token) => token.kind === 'string').map((token) => token.text))
      .toEqual(["'it''s'"])
  })

  it('块注释按嵌套深度闭合', () => {
    expect(sqlTokens('/* a /* b */ c */ SELECT')[0]).toEqual({ kind: 'comment', text: '/* a /* b */ c */' })
  })

  it('切分是无损的', () => {
    const sql = "SELECT count(*) FROM t WHERE a = $1 AND b IN ('x', 'y') -- 尾注"
    expect(sqlTokens(sql).map((token) => token.text).join('')).toBe(sql)
  })
})

describe('sqlSummary', () => {
  it('把多行压成一行', () => {
    expect(sqlSummary('SELECT id\n  FROM t\n  WHERE a = $1')).toBe('SELECT id FROM t WHERE a = $1')
  })

  it('超长才截断，并且带省略号', () => {
    expect(sqlSummary('SELECT a FROM t', 40)).toBe('SELECT a FROM t')
    expect(sqlSummary('SELECT abcdefghij FROM t WHERE x = $1', 10)).toBe('SELECT abc…')
  })
})

describe('formatSql', () => {
  it('按子句换行', () => {
    expect(formatSql('UPDATE pgbench_branches SET bbalance = bbalance + $1 WHERE bid = $2')).toBe(
      'UPDATE pgbench_branches\nSET bbalance = bbalance + $1\nWHERE bid = $2',
    )
  })

  it('连接条件与 AND 比所在子句缩一级', () => {
    expect(formatSql('SELECT a.id, b.name FROM a JOIN b ON a.id = b.a_id WHERE a.x = $1 AND b.y = $2 ORDER BY a.id')).toBe(
      [
        'SELECT a.id,',
        '  b.name',
        'FROM a',
        'JOIN b',
        '  ON a.id = b.a_id',
        'WHERE a.x = $1',
        '  AND b.y = $2',
        'ORDER BY a.id',
      ].join('\n'),
    )
  })

  it('子查询另起一块，函数调用不拆', () => {
    expect(formatSql('SELECT count(*) FROM (SELECT id FROM t WHERE x = $1) sub')).toBe(
      ['SELECT count(*)', 'FROM (', '  SELECT id', '  FROM t', '  WHERE x = $1', ') sub'].join('\n'),
    )
  })

  it('括号里的逗号不换行', () => {
    expect(formatSql('SELECT a FROM t WHERE id IN ($1, $2)')).toBe('SELECT a\nFROM t\nWHERE id IN ($1, $2)')
  })

  it('类型转换与限定名两侧不加空格', () => {
    expect(formatSql('SELECT s.a::text FROM s')).toBe('SELECT s.a::text\nFROM s')
  })

  it('只动空白：折行前后的记号序列一字不差', () => {
    const sql = "SELECT a.id, count(*) FROM a LEFT JOIN b ON b.a_id = a.id WHERE a.x = $1 GROUP BY a.id ORDER BY count(*) DESC LIMIT 10"
    const significant = (text: string) => sqlTokens(text).filter((token) => token.kind !== 'whitespace').map((token) => token.text)
    expect(significant(formatSql(sql))).toEqual(significant(sql))
  })

  it('空文本原样返回', () => {
    expect(formatSql('   ')).toBe('')
  })
})
