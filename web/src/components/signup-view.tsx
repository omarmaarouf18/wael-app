"use client";

import { useState } from "react";

import { ApiError, resendSignupOTP, signup, verifyOTP } from "@/lib/api";
import { dictionary, localePath, type Lang } from "@/lib/i18n";

import styles from "./auth.module.css";
import { SiteShell } from "./site-shell";

interface SignupPageViewProps {
  lang: Lang;
}

export function SignupPageView({ lang }: SignupPageViewProps) {
  const t = dictionary(lang);
  const text = t.auth.signup;
  const errors = t.auth.errors;

  const [step, setStep] = useState<1 | 2>(1);
  const [fullName, setFullName] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [pendingId, setPendingId] = useState("");
  const [otpCode, setOtpCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleStep1Submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setInfo(null);

    const trimmedName = fullName.trim();
    if (trimmedName.length < 2 || trimmedName.length > 100) {
      setError(errors.generic);
      return;
    }
    if (password.length < 8) {
      setError(errors.invalid_credentials);
      return;
    }
    if (password.length > 72) {
      setError(errors.password_too_long);
      return;
    }

    setLoading(true);
    try {
      const res = await signup({
        full_name: trimmedName,
        email: email.trim(),
        phone: phone.trim() || undefined,
        password,
      });
      setPendingId(res.pending_id);
      if (res.dev_otp) {
        setOtpCode(res.dev_otp);
      }
      setStep(2);
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.code === "conflict") {
          setError(errors.conflict);
        } else if (err.code === "password_too_long") {
          setError(errors.password_too_long);
        } else if (err.code === "too_many_attempts") {
          setError(errors.too_many_attempts);
        } else {
          setError(errors.generic);
        }
      } else {
        setError(errors.generic);
      }
    } finally {
      setLoading(false);
    }
  };

  const handleStep2Submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setInfo(null);

    if (otpCode.trim().length !== 6) {
      setError(errors.invalid_token);
      return;
    }

    setLoading(true);
    try {
      await verifyOTP({
        email: email.trim(),
        code: otpCode.trim(),
        pending_id: pendingId,
      });
      window.location.href = localePath(lang, "/account");
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.code === "invalid_token" || err.code === "unauthorized" || err.code === "bad_request") {
          setError(errors.invalid_token);
        } else if (err.code === "too_many_attempts") {
          setError(errors.too_many_attempts);
        } else {
          setError(errors.generic);
        }
      } else {
        setError(errors.generic);
      }
      setLoading(false);
    }
  };

  const handleResendOTP = async () => {
    setError(null);
    setInfo(null);
    try {
      const res = await resendSignupOTP({
        email: email.trim(),
      });
      if (res.dev_otp) {
        setOtpCode(res.dev_otp);
      }
      setInfo(text.resendSuccess);
    } catch {
      setError(errors.generic);
    }
  };

  return (
    <SiteShell lang={lang} path="/signup">
      <div className={styles.container}>
        <div className={styles.card}>
          {step === 1 ? (
            <>
              <h1 className={styles.title}>{text.title}</h1>
              <p className={styles.lead}>{text.lead}</p>

              {error ? <div className={styles.errorBanner} role="alert">{error}</div> : null}

              <form onSubmit={handleStep1Submit} className={styles.form}>
                <div className={styles.field}>
                  <label htmlFor="signup-name" className={styles.label}>
                    {text.fullNameLabel}
                  </label>
                  <input
                    id="signup-name"
                    type="text"
                    required
                    autoComplete="name"
                    placeholder={text.fullNamePlaceholder}
                    value={fullName}
                    onChange={(e) => setFullName(e.target.value)}
                    className={styles.input}
                  />
                </div>

                <div className={styles.field}>
                  <label htmlFor="signup-email" className={styles.label}>
                    {text.emailLabel}
                  </label>
                  <input
                    id="signup-email"
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
                  <label htmlFor="signup-phone" className={styles.label}>
                    {text.phoneLabel}
                  </label>
                  <input
                    id="signup-phone"
                    type="tel"
                    autoComplete="tel"
                    dir="ltr"
                    placeholder={text.phonePlaceholder}
                    value={phone}
                    onChange={(e) => setPhone(e.target.value)}
                    className={styles.input}
                  />
                </div>

                <div className={styles.field}>
                  <label htmlFor="signup-password" className={styles.label}>
                    {text.passwordLabel}
                  </label>
                  <input
                    id="signup-password"
                    type="password"
                    required
                    minLength={8}
                    maxLength={72}
                    autoComplete="new-password"
                    placeholder={text.passwordPlaceholder}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className={styles.input}
                  />
                </div>

                <p className={styles.legalNotice}>
                  {text.legalNoticePrefix}
                  <a
                    href={lang === "en" ? "https://legal.elmetracademy.app/terms#en" : "https://legal.elmetracademy.app/terms"}
                    target="_blank"
                    rel="noopener noreferrer"
                    className={styles.link}
                  >
                    {text.termsLink}
                  </a>
                  {text.legalNoticeAnd}
                  <a
                    href={lang === "en" ? "https://legal.elmetracademy.app/privacy#en" : "https://legal.elmetracademy.app/privacy"}
                    target="_blank"
                    rel="noopener noreferrer"
                    className={styles.link}
                  >
                    {text.privacyLink}
                  </a>
                  .
                </p>

                <button type="submit" disabled={loading} className={styles.submit}>
                  {loading ? text.submitting : text.submit}
                </button>

                <div className={styles.links}>
                  <span>{text.hasAccountPrompt}</span>
                  <a href={localePath(lang, "/login")} className={styles.link}>
                    {text.loginLink}
                  </a>
                </div>
              </form>
            </>
          ) : (
            <>
              <h1 className={styles.title}>{text.otpTitle}</h1>
              <p className={styles.lead}>{text.otpLead(email)}</p>

              {error ? <div className={styles.errorBanner} role="alert">{error}</div> : null}
              {info ? <div className={styles.successBanner} role="status">{info}</div> : null}

              <form onSubmit={handleStep2Submit} className={styles.form}>
                <div className={styles.field}>
                  <label htmlFor="signup-otp" className={styles.label}>
                    {text.otpCodeLabel}
                  </label>
                  <input
                    id="signup-otp"
                    type="text"
                    required
                    inputMode="numeric"
                    pattern="[0-9]{6}"
                    maxLength={6}
                    dir="ltr"
                    placeholder="123456"
                    value={otpCode}
                    onChange={(e) => setOtpCode(e.target.value)}
                    className={styles.input}
                  />
                </div>

                <button type="button" onClick={handleResendOTP} className={styles.secondaryButton}>
                  {text.resendOtp}
                </button>

                <button type="submit" disabled={loading} className={styles.submit}>
                  {loading ? text.otpSubmitting : text.otpSubmit}
                </button>
              </form>
            </>
          )}
        </div>
      </div>
    </SiteShell>
  );
}
