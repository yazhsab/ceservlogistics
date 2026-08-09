import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useNavigate } from "react-router-dom";
import {
  apiRequest,
  clearTokens,
  configureApiAuth,
  hasRefreshToken,
  setTokens,
  type TokenPair,
  type UserProfile,
} from "../api/client";

interface AuthContextValue {
  user?: UserProfile;
  isRestoring: boolean;
  login: (email: string, password: string) => Promise<UserProfile>;
  logout: (allDevices?: boolean) => Promise<void>;
  refreshProfile: () => Promise<void>;
  hasPermission: (permission?: string) => boolean;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<UserProfile>();
  const [isRestoring, setIsRestoring] = useState(hasRefreshToken());
  const navigate = useNavigate();

  const authenticationLost = useCallback(() => {
    setUser(undefined);
    void navigate("/login", { replace: true });
  }, [navigate]);

  useEffect(() => {
    configureApiAuth({
      onAuthenticationLost: authenticationLost,
    });
  }, [authenticationLost]);

  const refreshProfile = useCallback(async () => {
    setUser(await apiRequest<UserProfile>("/api/v1/auth/me"));
  }, []);

  useEffect(() => {
    if (!hasRefreshToken()) {
      setIsRestoring(false);
      return;
    }
    void apiRequest<UserProfile>("/api/v1/auth/me")
      .then(setUser)
      .catch(() => {
        clearTokens();
      })
      .finally(() => setIsRestoring(false));
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    const response = await apiRequest<{ tokens: TokenPair; user: UserProfile }>(
      "/api/v1/auth/login",
      {
        method: "POST",
        body: { email, password },
        auth: false,
      },
    );
    setTokens(response.tokens);
    setUser(response.user);
    return response.user;
  }, []);

  const logout = useCallback(
    async (allDevices = false) => {
      try {
        await apiRequest<void>("/api/v1/auth/logout", {
          method: "POST",
          body: { allDevices },
        });
      } finally {
        clearTokens();
        setUser(undefined);
        void navigate("/login", { replace: true });
      }
    },
    [navigate],
  );

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      isRestoring,
      login,
      logout,
      refreshProfile,
      hasPermission: (permission) =>
        !permission ||
        Boolean(user?.isSuperAdmin) ||
        Boolean(user?.permissions?.includes(permission)),
    }),
    [isRestoring, login, logout, refreshProfile, user],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) throw new Error("useAuth must be used inside AuthProvider");
  return context;
}
