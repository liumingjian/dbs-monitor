import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

const webRoot = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.dirname(webRoot)

function registeredContextFiles() {
  const document = fs.readFileSync(path.join(webRoot, 'CLAUDE.md'), 'utf8')
  const line = document.split(/\r?\n/).find((value) => value.includes('`createContext`') && value.includes('白名单'))
  if (!line) return new Set()
  return new Set([...line.matchAll(/`([^`]+)`/g)].map((match) => match[1]).filter((value) => value !== 'createContext'))
}

const noUnregisteredCreateContext = {
  meta: {
    type: 'problem',
    docs: { description: 'require createContext uses to be registered in web/CLAUDE.md' },
    schema: [],
    messages: { unregistered: 'createContext use must be registered in web/CLAUDE.md' },
  },
  create(context) {
    const relativePath = path.relative(repoRoot, context.filename).split(path.sep).join('/')
    const registered = registeredContextFiles().has(relativePath)
    if (registered) return {}

    return {
      ImportSpecifier(node) {
        if (node.imported.type === 'Identifier' && node.imported.name === 'createContext') {
          context.report({ node, messageId: 'unregistered' })
        }
      },
      CallExpression(node) {
        if (node.callee.type === 'Identifier' && node.callee.name === 'createContext') {
          context.report({ node, messageId: 'unregistered' })
        }
      },
      MemberExpression(node) {
        const property = node.property
        const isCreateContext = (!node.computed && property.type === 'Identifier' && property.name === 'createContext')
          || (node.computed && property.type === 'Literal' && property.value === 'createContext')
        if (isCreateContext) {
          context.report({ node, messageId: 'unregistered' })
        }
      },
    }
  },
}

export default tseslint.config(
  { ignores: ['dist', 'src/api/schema.d.ts'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { ecmaVersion: 2022, globals: globals.browser },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
      local: { rules: { 'no-unregistered-create-context': noUnregisteredCreateContext } },
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      ...reactRefresh.configs.vite.rules,
      'no-restricted-imports': ['error', { paths: ['redux', '@reduxjs/toolkit', 'zustand', 'jotai', 'mobx', 'valtio'] }],
      'no-restricted-syntax': ['error',
        { selector: 'LogicalExpression[operator="??"] > Literal.right[value=0]', message: '缺数不得补 0' },
        { selector: 'LogicalExpression[operator="||"] > Literal.right[value=0]', message: '缺数不得补 0' },
        { selector: 'CallExpression[callee.property.name="invalidateQueries"]', message: 'invalidateQueries 只许出现在 domain/<域>/mutations.ts' },
      ],
      'local/no-unregistered-create-context': 'error',
    },
  },
  {
    files: ['src/domain/**/*.{ts,tsx}', 'src/routes/**/*.{ts,tsx}'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
  {
    files: ['src/domain/*/mutations.ts'],
    rules: { 'no-restricted-syntax': 'off' },
  },
)
