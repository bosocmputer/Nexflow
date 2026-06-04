import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface UIState {
  sidebarCollapsed: boolean
  mobileNavOpen: boolean
  toggleSidebar: () => void
  setSidebarCollapsed: (v: boolean) => void
  toggleMobileNav: () => void
  setMobileNavOpen: (v: boolean) => void
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      mobileNavOpen: false,
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setSidebarCollapsed: (v) => set({ sidebarCollapsed: v }),
      toggleMobileNav: () => set((s) => ({ mobileNavOpen: !s.mobileNavOpen })),
      setMobileNavOpen: (v) => set({ mobileNavOpen: v }),
    }),
    {
      name: 'nexflow-ui-console',
      version: 2,
      migrate: (persistedState, version) => {
        const state = persistedState as { sidebarCollapsed?: boolean } | undefined
        if (!state || version < 2) {
          return { sidebarCollapsed: false }
        }
        return { sidebarCollapsed: state.sidebarCollapsed ?? false }
      },
      partialize: (state) => ({ sidebarCollapsed: state.sidebarCollapsed }),
    },
  ),
)
