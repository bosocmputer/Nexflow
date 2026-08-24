module.exports = {
  root: true,
  env: { browser: true, es2022: true },
  parser: '@typescript-eslint/parser',
  parserOptions: {
    ecmaVersion: 'latest',
    sourceType: 'module',
    ecmaFeatures: { jsx: true },
  },
  plugins: ['@typescript-eslint', 'react-hooks'],
  extends: ['eslint:recommended', 'plugin:@typescript-eslint/recommended'],
  ignorePatterns: ['dist', 'node_modules'],
  rules: {
    'no-constant-condition': 'off',
    // The legacy UI contains intentional control-character sanitizers and a
    // mixture of tabs/spaces. Keep the first production lint gate focused on
    // semantic JS/TS issues; formatting can be normalized separately.
    'no-control-regex': 'off',
    'no-misleading-character-class': 'off',
    'no-mixed-spaces-and-tabs': 'off',
    '@typescript-eslint/no-explicit-any': 'off',
    '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
  },
}
