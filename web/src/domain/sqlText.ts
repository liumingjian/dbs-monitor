/// SQL 文本的三种读法：列表里的**摘要**、详情里的**换行缩进**、着色用的**词法切分**。
///
/// 三件事都是纯字符串处理 —— 不认识 React、不取数、不认识路由。SQL 洞察、机群总览的
/// Top SQL 那一块、实例工作台的查询统计排行三处共用同一份，所以住在 `domain/` 而不是
/// 任何一个页面目录里。
///
/// **不引第三方 SQL 格式化 / 高亮库。** 这里要处理的全部是 pg_stat_statements 的归一化
/// 文本：字面量已经是 `$1` 占位符，没有多行字符串、没有存储过程体、没有方言分支，
/// 通用格式化器的绝大部分能力用不上，而它们的体积（连同各自的语法高亮包）会直接落到
/// 首屏。下面这三百行覆盖的正是这一种输入。
///
/// **格式化不改大小写。** 通常的「关键字大写」在这里是净损失：pg_stat_statements 回来的
/// 是开发者写下的原文，大小写本身是线索（有人靠它在代码库里 grep 到这条语句）。关键字
/// 由颜色区分就够了，不必再动一遍文本。

export type SqlTokenKind =
  | 'keyword'
  | 'string'
  | 'number'
  | 'parameter'
  | 'comment'
  | 'identifier'
  | 'operator'
  | 'whitespace'

export type SqlToken = { kind: SqlTokenKind; text: string }

/// 着色用的关键字表。刻意只收子句词与保留字：`name` / `value` / `status` 这类既是常见列名
/// 又出现在某些方言关键字表里的词一律不收 —— 把一个列名染成关键字，比不染更误导。
const keywords: ReadonlySet<string> = new Set([
  'ALL', 'ALTER', 'AND', 'ANY', 'AS', 'ASC', 'BEGIN', 'BETWEEN', 'BY', 'CASE', 'CAST',
  'COMMIT', 'CREATE', 'CROSS', 'DELETE', 'DESC', 'DISTINCT', 'DO', 'DROP', 'ELSE', 'END',
  'EXCEPT', 'EXISTS', 'FALSE', 'FETCH', 'FILTER', 'FOR', 'FROM', 'FULL', 'GROUP', 'HAVING',
  'ILIKE', 'IN', 'INNER', 'INSERT', 'INTERSECT', 'INTO', 'IS', 'JOIN', 'LATERAL', 'LEFT',
  'LIKE', 'LIMIT', 'NATURAL', 'NOT', 'NULL', 'NULLS', 'OFFSET', 'ON', 'OR', 'ORDER',
  'OUTER', 'OVER', 'PARTITION', 'RETURNING', 'RIGHT', 'ROLLBACK', 'ROWS', 'SELECT', 'SET',
  'THEN', 'TRUE', 'UNION', 'UPDATE', 'USING', 'VALUES', 'WHEN', 'WHERE', 'WINDOW', 'WITH',
])

/// 多字符运算符。先长后短匹配，否则 `->>` 会被切成 `->` 加 `>`，而 `::` 会被切成两个冒号 ——
/// 切碎之后格式化就没法把它们两边的空格收掉了。
const operators: readonly string[] = [
  '->>', '#>>', '::', ':=', '->', '#>', '@>', '<@', '<>', '!=', '<=', '>=', '||', '..',
]

const wordStart = /[A-Za-z_\u0080-\uffff]/
const wordRest = /[A-Za-z0-9_$\u0080-\uffff]/
const digit = /[0-9]/

/// 词法切分。空白与注释都保留成 token：详情弹窗要按原样把它们画出来，
/// 少一个空格，读者看到的就不是他写的那条语句了。
export function sqlTokens(sql: string): SqlToken[] {
  const tokens: SqlToken[] = []
  let index = 0

  const push = (kind: SqlTokenKind, text: string) => {
    tokens.push({ kind, text })
    index += text.length
  }

  while (index < sql.length) {
    const rest = sql.slice(index)
    const char = sql[index]

    const whitespace = /^\s+/.exec(rest)
    if (whitespace) {
      push('whitespace', whitespace[0])
      continue
    }

    if (rest.startsWith('--')) {
      const newline = rest.indexOf('\n')
      push('comment', newline === -1 ? rest : rest.slice(0, newline))
      continue
    }

    if (rest.startsWith('/*')) {
      push('comment', blockComment(rest))
      continue
    }

    if (char === "'") {
      push('string', quoted(rest, "'"))
      continue
    }

    // 双引号里是标识符而不是字符串：`"user"` 是一张表，染成字符串色会读反。
    if (char === '"') {
      push('identifier', quoted(rest, '"'))
      continue
    }

    if (char === '$') {
      const parameter = /^\$\d+/.exec(rest)
      if (parameter) {
        push('parameter', parameter[0])
        continue
      }
      const tag = /^\$(?:[A-Za-z_\u0080-\uffff][A-Za-z0-9_\u0080-\uffff]*)?\$/.exec(rest)
      if (tag) {
        const close = rest.indexOf(tag[0], tag[0].length)
        push('string', close === -1 ? rest : rest.slice(0, close + tag[0].length))
        continue
      }
      push('operator', char)
      continue
    }

    if (digit.test(char) || (char === '.' && digit.test(rest[1] ?? ''))) {
      const number = /^(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?/.exec(rest)
      // 正则以数字或 `.数字` 开头，上面的判断已经保证它一定命中。
      push('number', number ? number[0] : char)
      continue
    }

    if (wordStart.test(char)) {
      let end = 1
      while (end < rest.length && wordRest.test(rest[end])) end += 1
      const word = rest.slice(0, end)
      push(keywords.has(word.toUpperCase()) ? 'keyword' : 'identifier', word)
      continue
    }

    const operator = operators.find((candidate) => rest.startsWith(candidate))
    push('operator', operator ?? char)
  }

  return tokens
}

/// 块注释。PostgreSQL 的块注释可以嵌套，所以要数深度而不是找第一个 `*/`。
function blockComment(rest: string): string {
  let depth = 0
  let index = 0
  while (index < rest.length) {
    if (rest.startsWith('/*', index)) {
      depth += 1
      index += 2
      continue
    }
    if (rest.startsWith('*/', index)) {
      depth -= 1
      index += 2
      if (depth === 0) return rest.slice(0, index)
      continue
    }
    index += 1
  }
  return rest // 没闭合就吃到末尾：残缺的注释不该把后面的语句拖成别的词类。
}

/// 引号包裹的一段。同种引号写两遍是转义（`''` / `""`），不是结束。
/// 反斜杠不参与转义：`standard_conforming_strings` 打开时 PostgreSQL 的 `'a\'` 就是完整的一段。
function quoted(rest: string, quote: string): string {
  let index = 1
  while (index < rest.length) {
    if (rest[index] !== quote) {
      index += 1
      continue
    }
    if (rest[index + 1] === quote) {
      index += 2
      continue
    }
    return rest.slice(0, index + 1)
  }
  return rest
}

const summaryLimit = 120

/// 列表格子里的摘要：压成一行，超长就截断。
///
/// 40px 的行放不下第二行，而一条 SQL 里最靠前的那几十个字符（动词 + 主表）已经足以
/// 认出「这是哪一条」；剩下的归详情弹窗。截断按码点切，不会把一个字符劈成两半。
export function sqlSummary(sql: string, limit: number = summaryLimit): string {
  const collapsed = sql.replace(/\s+/g, ' ').trim()
  const characters = [...collapsed]
  if (characters.length <= limit) return collapsed
  return `${characters.slice(0, limit).join('').trimEnd()}…`
}

/// 换行处触发的子句词。先长后短匹配：`LEFT JOIN` 要在 `LEFT` 处换行而不是在 `JOIN` 处，
/// 否则 `LEFT` 会孤零零留在上一行的末尾。
const clauses: readonly (readonly string[])[] = [
  ['LEFT', 'OUTER', 'JOIN'], ['RIGHT', 'OUTER', 'JOIN'], ['FULL', 'OUTER', 'JOIN'],
  ['INSERT', 'INTO'], ['DELETE', 'FROM'], ['GROUP', 'BY'], ['ORDER', 'BY'], ['UNION', 'ALL'],
  ['INNER', 'JOIN'], ['LEFT', 'JOIN'], ['RIGHT', 'JOIN'], ['FULL', 'JOIN'], ['CROSS', 'JOIN'],
  ['NATURAL', 'JOIN'], ['JOIN'],
  ['WITH'], ['SELECT'], ['FROM'], ['WHERE'], ['HAVING'], ['WINDOW'], ['LIMIT'], ['OFFSET'],
  ['FETCH'], ['UNION'], ['INTERSECT'], ['EXCEPT'], ['VALUES'], ['UPDATE'], ['SET'],
  ['RETURNING'], ['ON'], ['AND'], ['OR'],
]

/// 比所在子句再缩一级的连接词：条件挂在它上面那一行，缩进把这层从属关系说出来。
const continuations: ReadonlySet<string> = new Set(['ON', 'AND', 'OR'])

/// 子查询会另起一块的开括号。判据是括号后面第一个词 —— `count(` 后面不是子句词，
/// 于是它照旧留在一行里，只有 `(SELECT` / `(VALUES` / `(WITH` 才展开。
const blockStarters: ReadonlySet<string> = new Set(['SELECT', 'VALUES', 'WITH'])

/// 格式化：按子句换行、按括号缩进。只动空白，一个字符都不增删。
///
/// 折行只发生在子句词与顶层逗号处，不做「每列一行」这种更激进的排版：归一化文本里的选择
/// 列表常常有二三十项，一项一行会把弹窗撑成一页滚动条，而读者要找的子句反而被推出屏幕。
export function formatSql(sql: string): string {
  const significant = sqlTokens(sql).filter((token) => token.kind !== 'whitespace')
  if (significant.length === 0) return sql.trim()

  const lines: string[] = []
  let current = ''
  /// 缩进是在**行首**定下来的。子句词决定这一行缩几级，行尾再问一次就已经晚了 ——
  /// 中途遇到的括号会把它改掉。
  let currentIndent = 0
  let indent = 0
  /// 每个未闭合括号是不是「另起一块」的那种。闭括号照着它决定要不要回退缩进。
  const blocks: boolean[] = []

  const flush = () => {
    if (current !== '') lines.push('  '.repeat(Math.max(currentIndent, 0)) + current)
    current = ''
  }
  const begin = (level: number, text: string) => {
    currentIndent = level
    current = text
  }

  for (let index = 0; index < significant.length; index += 1) {
    const token = significant[index]
    const previous = significant[index - 1]
    const clause = current === '' ? null : matchClause(significant, index)

    if (clause !== null) {
      flush()
      const level = continuations.has(clause[0].text.toUpperCase()) ? indent + 1 : indent
      begin(level, clause.map((item) => item.text).join(' '))
      index += clause.length - 1
      continue
    }

    const glue = current === '' ? '' : separator(previous, token)

    if (token.kind === 'operator' && token.text === '(') {
      const next = significant[index + 1]
      const block = next !== undefined && next.kind === 'keyword' && blockStarters.has(next.text.toUpperCase())
      blocks.push(block)
      current += glue + token.text
      if (block) {
        flush()
        indent += 1
        currentIndent = indent
      }
      continue
    }

    if (token.kind === 'operator' && token.text === ')') {
      if (blocks.pop() === true) {
        flush()
        indent = Math.max(indent - 1, 0)
        begin(indent, token.text)
      } else {
        current += token.text
      }
      continue
    }

    current += glue + token.text

    // 顶层逗号才换行。括号里的逗号是参数分隔，拆开只会让一次函数调用横跨五行。
    if (token.kind === 'operator' && token.text === ',' && blocks.length === 0) {
      flush()
      currentIndent = indent + 1
    }
  }

  flush()
  return lines.join('\n')
}

/// 两个 token 之间要不要空格。
function separator(previous: SqlToken | undefined, token: SqlToken): string {
  if (previous === undefined || previous.text === '') return ''
  const before = previous.text
  const after = token.text

  if (after === ',' || after === ';' || after === ')' || after === '.') return ''
  if (before === '(' || before === '.') return ''
  if (before === '::' || after === '::') return ''
  // 函数调用的括号紧贴函数名；`IN (` / `VALUES (` 这类跟在关键字后面的括号照常留空格。
  if (after === '(' && previous.kind === 'identifier') return ''
  return ' '
}

/// 从 `index` 处能不能读出一个子句词组。先长后短，读出来就把整组一起交出去。
function matchClause(tokens: readonly SqlToken[], index: number): SqlToken[] | null {
  if (tokens[index].kind !== 'keyword') return null
  for (const clause of clauses) {
    if (clause.length > tokens.length - index) continue
    const matched = clause.every((word, offset) => {
      const candidate = tokens[index + offset]
      return candidate.kind === 'keyword' && candidate.text.toUpperCase() === word
    })
    if (matched) return tokens.slice(index, index + clause.length)
  }
  return null
}
