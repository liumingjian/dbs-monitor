export function parseDomainRegistry(document: string): string[] {
  const line = document.split(/\r?\n/).find((value) => value.includes('`domain/`') && value.includes('封闭清单'))
  if (!line) throw new Error('web/CLAUDE.md is missing the domain registry')

  return [...line.matchAll(/`([^`]+)`/g)]
    .map((match) => match[1])
    .filter((name) => name !== 'domain/')
    .sort()
}
