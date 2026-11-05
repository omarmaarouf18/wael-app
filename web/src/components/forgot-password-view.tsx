"use client";

import { useState } from "react";

import { ApiError, confirmPasswordReset, requestPasswordReset, verifyPasswordReset } from "@/lib/api";
import { dictionary, localePath, type Lang } from "@/lib/i18n";

import styles from "./auth.module.css";
import { SiteShell } from "./site-shell";

interface ForgotPasswordPageViewProps {
  lang: Lang;
}

export function ForgotPasswordPageView({ lang }: ForgotPasswordPageViewProps) {
  const t = dictionary(lang);
  const text = t.auth.forgotPassword;
  const errors = t.auth.errors;

  const [step, setStep] = useState<1 | 2 | 3 | 4>(1);
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [resetToken, setResetToken] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleStep1 = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const res = await requestPasswordReset(email.trim());
      if (res.dev_otp) {
        setCode(res.dev_otp);
      }
      setStep(2);
    } catch {
      // Always advance anti-enumeration
      setStep(2);
    } finally {
      setLoading(false);
    }
  };

  const handleStep2 = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (code.trim().length !== 6) {
      setError(errors.invalid_token);
      return;
    }
    setLoading(true);
    try {
      const res = await verifyPasswordReset(email.trim(), code.trim());
      setResetToken(res.reset_token);
      setStep(3);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.code === "too_many_attempts") {
        setError(errors.too_many_attempts);
      } else {
        setError(errors.invalid_token);
      }
    } finally {
      setLoading(false);
    }
  };

  const handleStep3 = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (newPassword.length < 8) {
      setError(errors.invalid_credentials);
      return;
    }
    if (newPassword.length > 72) {
      setError(errors.password_too_long);
      return;
    }
    if (newPassword !== confirmPassword) {
      setError(text.passwordMismatch);
      return;
    }
    setLoading(true);
    try {
      await confirmPasswordReset({
        reset_token: resetToken,
        new_password: newPassword,
      });
      setStep(4);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.code === "password_too_long") {
        setError(errors.password_too_long);
      } else {
        setError(errors.generic);
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <SiteShell lang={lang} path="/forgot-password">
      <div className={styles.container}>
        <div className={styles.card}>
          {step === 1 ? (
            <>
              <h1 className={styles.title}>{text.step1Title}</h1>
              <p className={styles.lead}>{text.step1Lead}</p>
              {error ? <div className={styles.errorBanner}>{error}</div> : null}
              <form onSubmit={handleStep1} className={styles.form}>
                <div className={styles.field}>
                  <label htmlFor="reset-email" className={styles.label}>
                    {t.auth.login.emailLabel}
                  </label>
                  <input
                    id="reset-email"
                    type="email"
                    required
                    autoComplete="email"
                    dir="ltr"
                    placeholder={t.auth.login.emailPlaceholder}
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    className={styles.input}
                  />
                </div>
                <button type="submit" disabled={loading} className={styles.submit}>
                  {loading ? text.sendingCode : text.sendCodeSubmit}
                </button>
                <div className={styles.links}>
                  <a href={localePath(lang, "/login")} className={styles.link}>
                    {text.backToLogin}
                  </a>
                </div>
              </form>
            </>
          ) : step === 2 ? (
            <>
              <h1 className={styles.title}>{text.step2Title}</h1>
              <p className={styles.lead}>{text.step2Lead(email)}</p>
              {error ? <div className={styles.errorBanner}>{error}</div> : null}
              <form onSubmit={handleStep2} className={styles.form}>
                <div className={styles.field}>
                  <label htmlFor="reset-code" className={styles.label}>
                    {t.auth.signup.otpCodeLabel}
                  </label>
                  <input
                    id="reset-code"
                    type="text"
                    required
                    inputMode="numeric"
                    pattern="[0-9]{6}"
                    maxLength={6}
                    dir="ltr"
                    placeholder="123456"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                    className={styles.input}
                  />
                </div>
                <button type="submit" disabled={loading} className={styles.submit}>
                  {loading ? t.auth.signup.otpSubmitting : text.verifyCodeSubmit}
                </button>
              </form>
            </>
          ) : step === 3 ? (
            <>
              <h1 className={styles.title}>{text.step3Title}</h1>
              <p className={styles.lead}>{text.step3Lead}</p>
              {error ? <div className={styles.errorBanner}>{error}</div> : null}
              <form onSubmit={handleStep3} className={styles.form}>
                <div className={styles.field}>
                  <label htmlFor="new-password" className={styles.label}>
                    {text.newPasswordLabel}
                  </label>
                  <input
                    id="new-password"
                    type="password"
                    required
                    minLength={8}
                    maxLength={72}
                    autoComplete="new-password"
                    placeholder={t.auth.signup.passwordPlaceholder}
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    className={styles.input}
                  />
                </div>
                <div className={styles.field}>
                  <label htmlFor="confirm-password" className={styles.label}>
                    {text.confirmPasswordLabel}
                  </label>
                  <input
                    id="confirm-password"
                    type="password"
                    required
                    minLength={8}
                    maxLength={72}
                    autoComplete="new-password"
                    placeholder={t.auth.signup.passwordPlaceholder}
                    value={confirmPassword}
                    onChange={(e) => setConfirmPassword(e.target.value)}
                    className={styles.input}
                  />
                </div>
                <button type="submit" disabled={loading} className={styles.submit}>
                  {loading ? t.auth.signup.otpSubmitting : text.resetSubmit}
                </button>
              </form>
            </>
          ) : (
            <>
              <h1 className={styles.title}>{text.step1Title}</h1>
              <div className={styles.successBanner}>{text.resetSuccess}</div>
              <div className={styles.links}>
                <a href={localePath(lang, "/login")} className={styles.link}>
                  {text.backToLogin}
                </a>
              </div>
            </>
          )}
        </div>
      </div>
    </SiteShell>
  );
}
