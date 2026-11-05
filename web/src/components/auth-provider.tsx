"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import {
  clearAccessToken,
  getAccessToken,
  getMe,
  logout,
  refresh,
  setAccessToken,
  type UserProfile,
} from "@/lib/api";
import { dictionary, localePath, type Lang } from "@/lib/i18n";

import styles from "./site-shell.module.css";

export interface AuthContextValue {
  isLoggedIn: boolean;
  isLoading: boolean;
  user: UserProfile | null;
  token: string | null;
  refreshAuth: () => Promise<boolean>;
  logoutUser: () => Promise<void>;
  setUser: (user: UserProfile | null) => void;
  setToken: (token: string | null) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

const emptySubscribe = () => () => {};

function useIsClient(): boolean {
  return useSyncExternalStore(
    emptySubscribe,
    () => true,
    () => false,
  );
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [user, setUser] = useState<UserProfile | null>(null);
  const [token, setTokenState] = useState<string | null>(getAccessToken());

  const setToken = useCallback((newToken: string | null) => {
    setAccessToken(newToken);
    setTokenState(newToken);
    setIsLoggedIn(Boolean(newToken));
  }, []);

  const refreshAuth = useCallback(async (): Promise<boolean> => {
    try {
      const res = await refresh();
      setTokenState(res.access_token);
      setIsLoggedIn(true);

      // Fetch profile in background if available
      try {
        const me = await getMe();
        setUser(me);
      } catch {
        // Non-fatal if profile fetch fails
      }
      return true;
    } catch {
      // 401 or any error from refresh means logged out, silently.
      clearAccessToken();
      setTokenState(null);
      setIsLoggedIn(false);
      setUser(null);
      return false;
    } finally {
      setIsLoading(false);
    }
  }, []);

  const logoutUser = useCallback(async () => {
    try {
      await logout();
    } catch {
      // Ignore network errors on logout
    } finally {
      clearAccessToken();
      setTokenState(null);
      setIsLoggedIn(false);
      setUser(null);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function initAuth() {
      try {
        const res = await refresh();
        if (cancelled) return;
        setTokenState(res.access_token);
        setIsLoggedIn(true);
        try {
          const me = await getMe();
          if (!cancelled) setUser(me);
        } catch {
          // Non-fatal
        }
      } catch {
        if (cancelled) return;
        clearAccessToken();
        setTokenState(null);
        setIsLoggedIn(false);
        setUser(null);
      } finally {
        if (!cancelled) {
          setIsLoading(false);
        }
      }
    }

    void initAuth();

    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <AuthContext.Provider
      value={{
        isLoggedIn,
        isLoading,
        user,
        token,
        refreshAuth,
        logoutUser,
        setUser,
        setToken,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return ctx;
}

export function HeaderAuthNav({ lang }: { lang: Lang }) {
  const t = dictionary(lang);
  const { isLoggedIn, isLoading, logoutUser } = useAuth();
  const isClient = useIsClient();
  const to = (p: string) => localePath(lang, p);

  // During SSR or before client mount, render the default unauthenticated links
  // so static prerendering matches without hydration mismatch.
  if (!isClient || isLoading) {
    return (
      <>
        <a href={to("/login")} className={styles.navLink}>
          {t.nav.login}
        </a>
        <a href={to("/signup")} className={styles.cta}>
          {t.nav.signup}
        </a>
      </>
    );
  }

  if (isLoggedIn) {
    return (
      <>
        <a href={to("/account")} className={styles.navLink}>
          {t.nav.account}
        </a>
        <button
          type="button"
          onClick={() => {
            void logoutUser().then(() => {
              if (window.location.pathname === to("/account")) {
                window.location.href = to("/login");
              }
            });
          }}
          className={styles.logoutBtn}
        >
          {t.nav.logout}
        </button>
      </>
    );
  }

  return (
    <>
      <a href={to("/login")} className={styles.navLink}>
        {t.nav.login}
      </a>
      <a href={to("/signup")} className={styles.cta}>
        {t.nav.signup}
      </a>
    </>
  );
}
