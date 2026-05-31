import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type Theme = 'light' | 'dark' | 'system'

interface ThemeState {
  theme: Theme
  setTheme: (t: Theme) => void
}

const apply = (theme: Theme) => {
  const root = document.documentElement
  const resolved =
    theme === 'system'
      ? window.matchMedia('(prefers-color-scheme: dark)').matches
        ? 'dark'
        : 'light'
      : theme
  root.classList.toggle('dark', resolved === 'dark')
}

export const useTheme = create<ThemeState>()(
  persist(
    (set) => ({
      theme: 'light',
      setTheme: (t) => {
        apply(t)
        set({ theme: t })
      },
    }),
    {
      name: 'nexflow-theme',
      onRehydrateStorage: () => (state) => {
        if (state) apply(state.theme)
      },
    },
  ),
)

if (typeof window !== 'undefined') {
  window
    .matchMedia('(prefers-color-scheme: dark)')
    .addEventListener('change', () => {
      const t = useTheme.getState().theme
      if (t === 'system') apply('system')
    })
}
