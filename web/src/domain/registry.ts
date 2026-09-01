const NAME_PATTERN = /`([^`]+)`/g

function namesOn(line: string): string[] {
  return [...line.matchAll(NAME_PATTERN)].map((match) => match[1]).filter((name) => name !== 'domain/')
}

export function parseDomainRegistry(document: string): string[] {
  const lines = document.split(/\r?\n/)
  // The registry is announced by a line naming `domain/` as a closed list. The names themselves sit on
  // that line or, when the announcement is a heading, on the first line carrying names below it.
  const start = lines.findIndex((value) => value.includes('`domain/`') && value.includes('封闭清单'))
  if (start === -1) throw new Error('web/CLAUDE.md is missing the domain registry')

  const registry = lines.slice(start).find((line) => namesOn(line).length > 0)
  if (!registry) throw new Error('web/CLAUDE.md declares the domain registry but lists no components')

  return namesOn(registry).sort()
}
