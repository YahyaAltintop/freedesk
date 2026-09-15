import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'

// Flat ESLint config for the Vue 3 + TypeScript client.
export default defineConfigWithVueTs(
  {
    name: 'app/files-to-lint',
    files: ['**/*.{ts,vue}'],
  },
  {
    name: 'app/files-to-ignore',
    ignores: ['dist/**', 'dist-ssr/**'],
  },
  pluginVue.configs['flat/essential'],
  vueTsConfigs.recommended,
  {
    // App.vue is the legitimately single-word root component.
    name: 'app/overrides',
    files: ['src/App.vue'],
    rules: {
      'vue/multi-word-component-names': 'off',
    },
  },
)
