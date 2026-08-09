import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { ApiError } from "./api/client";
import { AuthProvider } from "./auth/AuthProvider";
import { ToastProvider } from "./components/ToastProvider";
import { AppErrorBoundary } from "./components/AppErrorBoundary";
import "./styles.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        if (error instanceof ApiError) {
          if ([400, 401, 403, 404, 409, 422].includes(error.status))
            return false;
          return failureCount < 1 && (error.status === 429 || error.status >= 500);
        }
        return failureCount < 1;
      },
      refetchOnWindowFocus: false,
      staleTime: 20_000,
    },
    mutations: { retry: false },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppErrorBoundary>
      <BrowserRouter>
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ToastProvider>
              <App />
            </ToastProvider>
          </AuthProvider>
        </QueryClientProvider>
      </BrowserRouter>
    </AppErrorBoundary>
  </StrictMode>,
);
