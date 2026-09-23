import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Toaster } from '@/components/ui/sonner'
import { ThemeProvider } from '@/components/theme-provider'
import { ScopeProvider } from '@/components/scope-provider'
import { router } from '@/router'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 15_000,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <ScopeProvider>
          <TooltipProvider delayDuration={250} skipDelayDuration={400}>
            <RouterProvider router={router} />
            <Toaster />
          </TooltipProvider>
        </ScopeProvider>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
)
