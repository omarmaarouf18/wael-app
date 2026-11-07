"use client";

import { useState } from "react";

import { ApiError, login } from "@/lib/api";
import { dictionary, localePath, type Lang } from "@/lib/i18n";

import styles from "./auth.module.css";
import { SiteShell } from "./site-shell";

interface LoginPageViewProps {
  lang: Lang;
}

export function LoginPageView({ lang }: LoginPageViewProps) {
  const t = dictionary(lang);
  const text = t.auth.login;
  const errors = t.auth.errors;

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await login({ email: email.trim(), password });
      window.location.href = localePath(lang, "/account");
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.code === "account_locked") {
          setError(errors.account_locked);
        } else if (err.code === "too_many_attempts") {
          setError(errors.too_many_attempts);
        } else if (err.code === "unauthorized" || err.code === "invalid_credentials") {
          setError(errors.invalid_credentials);
        } else {
          setError(errors.generic);
        }
      } else {
        setError(errors.generic);
      }
      setLoading(false);
    }
  };

  return (
    <SiteShell lang={lang} path="/login">
      <div className={styles.container}>
        <div className={styles.card}>
          <h1 className={styles.title}>{text.title}</h1>
          <p className={styles.lead}>{text.lead}</p>

          {error ? <div className={styles.errorBanner} role="alert">{error}</div> : null}

          <form onSubmit={handleSubmit} className={styles.form}>
            <div className={styles.field}>
              <label htmlFor="login-email" className={styles.label}>
                {text.emailLabel}
              </label>
              <input
                id="login-email"
                type="email"
                required
                autoComplete="email"
                dir="ltr"
                placeholder={text.emailPlaceholder}
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className={styles.input}
              />
            </div>

            <div className={styles.field}>
              <label htmlFor="login-password" className={styles.label}>
                {text.passwordLabel}
              </label>
              <input
                id="login-password"
                type="password"
                required
                autoComplete="current-password"
                placeholder={text.passwordPlaceholder}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className={styles.input}
              />
            </div>

            <div className={styles.links}>
              <a href={localePath(lang, "/forgot-password")} className={styles.link}>
                {text.forgotPasswordLink}
              </a>
            </div>

            <button type="submit" disabled={loading} className={styles.submit}>
              {loading ? text.submitting : text.submit}
            </button>

            <div className={styles.links}>
              <span>{text.noAccountPrompt}</span>
              <a href={localePath(lang, "/signup")} className={styles.link}>
                {text.signupLink}
              </a>
            </div>
          </form>
        </div>
      </div>
    </SiteShell>
  );
}
