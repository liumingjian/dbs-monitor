import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'src/api/schema.d.ts'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { ecmaVersion: 2022, globals: globals.browser },
    plugins: { 'react-hooks': reactHooks, 'react-refresh': reactRefresh },
    rules: {
      ...reactHooks.configs.recommended.rules,
      ...reactRefresh.configs.vite.rules,
      'no-restricted-imports': ['error', { paths: ['redux', '@reduxjs/toolkit', 'zustand', 'jotai', 'mobx', 'valtio'] }],
      'no-restricted-syntax': ['error',
        { selector: 'LogicalExpression[operator="??"] > Literal.right[value=0]', message: '缺数不得补 0' },
        { selector: 'LogicalExpression[operator="||"] > Literal.right[value=0]', message: '缺数不得补 0' },
        { selector: 'CallExpression[callee.property.name="invalidateQueries"]', message: 'invalidateQueries 只许出现在 domain/<域>/mutations.ts' },
      ],
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
