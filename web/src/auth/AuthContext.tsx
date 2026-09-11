import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { useNavigate } from 'react-router-dom';
import type { AuthUser } from '../types/auth';
import { clearAuthStorage, getStoredUser, login as apiLogin } from '../api/client';

interface AuthContextValue {
  user: AuthUser | null;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue>({
  user: null,
  login: async () => {},
  logout: () => {},
});

export function useAuth(): AuthContextValue {
  return useContext(AuthContext);
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(() => getStoredUser());
  const navigate = useNavigate();

  const login = useCallback(
    async (username: string, password: string) => {
      const tokens = await apiLogin({ username, password });
      setUser(tokens.user);
      navigate('/', { replace: true });
    },
    [navigate],
  );

  const logout = useCallback(() => {
    clearAuthStorage();
    setUser(null);
    navigate('/login', { replace: true });
  }, [navigate]);

  const value = useMemo(() => ({ user, login, logout }), [user, login, logout]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}