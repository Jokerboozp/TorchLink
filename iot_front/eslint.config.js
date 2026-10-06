import js from '@eslint/js'
import vue from 'eslint-plugin-vue'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default [
  { ignores: ['dist/**', 'node_modules/**'] },
  js.configs.recommended,
  ...vue.configs['flat/essential'],
  {
    languageOptions: { ecmaVersion: 'latest', sourceType: 'module', globals: { ...globals.browser, ...globals.node } },
    rules: {
      'no-unused-vars': ['error', { args: 'none', caughtErrors: 'none', ignoreRestSiblings: true }],
      // 解析与清理输入时有意匹配控制字符。
      'no-control-regex': 'off',
      // 编辑子对象字段是现有表单组件的约定；禁止的只是整体替换 prop。
      'vue/no-mutating-props': ['error', { shallowOnly: true }],
      'vue/multi-word-component-names': 'off'
    }
  },
  // TypeScript sources; .vue files keep vue-eslint-parser and parse <script lang="ts"> with the TS parser.
  ...tseslint.configs.recommended.map(config => ({ ...config, files: ['**/*.ts'] })),
  { files: ['**/*.vue'], languageOptions: { parserOptions: { parser: tseslint.parser } } },
  {
    files: ['**/*.ts'],
    rules: {
      // Response bodies and loosely shaped Harness events are typed as any on purpose.
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': ['error', { args: 'none', caughtErrors: 'none', ignoreRestSiblings: true }]
    }
  }
]
