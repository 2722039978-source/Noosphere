import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter } from "react-router-dom";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ToastProvider } from "@/components/ui/toast";
import { BackendProvider } from "@/hooks/useBackend";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

export function AppProviders({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <BackendProvider>
        <TooltipProvider>
          <ToastProvider>
            <HashRouter>{children}</HashRouter>
          </ToastProvider>
        </TooltipProvider>
      </BackendProvider>
    </QueryClientProvider>
  );
}
