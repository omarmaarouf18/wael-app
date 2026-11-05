"use client";

import { useEffect, useState } from "react";

import {
  apiGet,
  createPurchaseRequest,
  getMyEntitlements,
  getMyRequests,
  getPaymentMethods,
  refresh,
  type PaymentMethod,
  type StudentRequestItem,
} from "@/lib/api";
import { dictionary, localePath, type Lang } from "@/lib/i18n";

import styles from "./auth.module.css";

interface SubjectBuySectionProps {
  lang: Lang;
  subjectId: string;
  price: number;
}

interface AppConfig {
  update_url_android?: string;
  update_url_ios?: string;
  update_url?: string;
}

const DEFAULT_ANDROID_URL = "https://play.google.com/store/apps/details?id=app.elmetracademy.elmetr";
const DEFAULT_IOS_URL = "https://apps.apple.com/app/el-metr-academy/id6737155342";

export function SubjectBuySection({ lang, subjectId, price }: SubjectBuySectionProps) {
  const t = dictionary(lang);
  const text = t.catalog;

  const [loading, setLoading] = useState(true);
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [isOwned, setIsOwned] = useState(false);
  const [pendingRequest, setPendingRequest] = useState<StudentRequestItem | null>(null);
  const [paymentMethods, setPaymentMethods] = useState<PaymentMethod[]>([]);
  const [storeUrls, setStoreUrls] = useState({ android: DEFAULT_ANDROID_URL, ios: DEFAULT_IOS_URL });

  // Form state
  const [selectedMethodIdx, setSelectedMethodIdx] = useState(0);
  const [transferRef, setTransferRef] = useState("");
  const [senderNumber, setSenderNumber] = useState("");
  const [amount, setAmount] = useState(price);
  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function checkState() {
      try {
        await refresh();
      } catch {
        if (!cancelled) {
          setIsLoggedIn(false);
          setLoading(false);
        }
        return;
      }

      if (cancelled) return;
      setIsLoggedIn(true);

      try {
        const [ents, reqs, methodsRes, configData] = await Promise.all([
          getMyEntitlements().catch(() => ({ subject_ids: [] as string[] })),
          getMyRequests().catch(() => ({ items: [] as StudentRequestItem[] })),
          getPaymentMethods().catch(() => ({ items: [] as PaymentMethod[] })),
          apiGet<AppConfig>("/api/v1/academy/app-config").catch(() => ({} as AppConfig)),
        ]);

        if (cancelled) return;

        if (ents.subject_ids.includes(subjectId)) {
          setIsOwned(true);
        }

        const pending = reqs.items?.find(
          (r) => r.subject_id === subjectId && r.status === "pending",
        );
        if (pending) {
          setPendingRequest(pending);
        }

        if (Array.isArray(methodsRes.items)) {
          setPaymentMethods(methodsRes.items);
        }

        if (configData) {
          setStoreUrls({
            android: configData.update_url_android || configData.update_url || DEFAULT_ANDROID_URL,
            ios: configData.update_url_ios || DEFAULT_IOS_URL,
          });
        }
      } catch {
        // Fallback to defaults
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    void checkState();

    return () => {
      cancelled = true;
    };
  }, [subjectId]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;

    const trimmedRef = transferRef.trim();
    const trimmedSender = senderNumber.trim();
    if (trimmedRef.length < 3 || trimmedRef.length > 100) {
      setErrorMsg(text.buyErrors.invalid_reference);
      return;
    }
    if (trimmedSender.length < 3 || trimmedSender.length > 50) {
      setErrorMsg(text.buyErrors.invalid_sender);
      return;
    }
    if (amount <= 0) {
      setErrorMsg(text.buyErrors.invalid_amount);
      return;
    }

    const method = paymentMethods[selectedMethodIdx];
    if (!method) {
      setErrorMsg(text.noPaymentMethodsConfigured);
      return;
    }

    setSubmitting(true);
    setErrorMsg(null);
    setSuccessMsg(null);

    try {
      const created = await createPurchaseRequest(subjectId, {
        payment_method: method.kind,
        transfer_reference: trimmedRef,
        sender_number: trimmedSender,
        amount,
      });

      setPendingRequest({
        id: created.id,
        subject_id: created.subject_id,
        subject_title_ar: "",
        subject_title_en: "",
        status: "pending",
        source: created.source,
        created_at: created.created_at,
        amount: created.amount,
      });
      setSuccessMsg(text.purchaseRequestSuccess);
    } catch (err: unknown) {
      if (err && typeof err === "object" && "code" in err) {
        const code = String(err.code);
        if (code in text.buyErrors) {
          setErrorMsg(text.buyErrors[code as keyof typeof text.buyErrors]);
          return;
        }
      }
      setErrorMsg(text.buyErrors.generic);
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return null;
  }

  if (!isLoggedIn) {
    return (
      <div className={styles.buySection}>
        <div className={styles.studyBanner}>
          <h3 className={styles.studyBannerTitle}>{text.buyFormTitle}</h3>
          <p className={styles.studyBannerLead}>{text.loginToBuyPrompt}</p>
          <div className={styles.storeButtons}>
            <a href={localePath(lang, "/login")} className={styles.submit}>
              {text.loginButton}
            </a>
            <a href={localePath(lang, "/signup")} className={styles.secondaryButton}>
              {text.signupButton}
            </a>
          </div>
        </div>
      </div>
    );
  }

  if (isOwned) {
    return (
      <div className={styles.buySection}>
        <div className={styles.studyBanner}>
          <h3 className={styles.studyBannerTitle}>{text.ownedSubjectBannerTitle}</h3>
          <p className={styles.studyBannerLead}>{text.ownedSubjectBannerLead}</p>
          <div className={styles.storeButtons}>
            <a
              href={storeUrls.android}
              target="_blank"
              rel="noopener noreferrer"
              className={styles.storeButton}
            >
              {t.auth.account.downloadAndroid}
            </a>
            <a
              href={storeUrls.ios}
              target="_blank"
              rel="noopener noreferrer"
              className={styles.storeButton}
            >
              {t.auth.account.downloadIos}
            </a>
          </div>
        </div>
      </div>
    );
  }

  if (pendingRequest) {
    return (
      <div className={styles.buySection}>
        <div className={styles.studyBanner}>
          <h3 className={styles.studyBannerTitle}>{text.pendingRequestBannerTitle}</h3>
          <p className={styles.studyBannerLead}>{text.pendingRequestBannerLead}</p>
          {successMsg ? <div className={styles.successBanner}>{successMsg}</div> : null}
        </div>
      </div>
    );
  }

  return (
    <div className={styles.buySection}>
      <div className={styles.card}>
        <h3 className={styles.title}>{text.buyFormTitle}</h3>
        <p className={styles.lead}>{text.price(String(price))}</p>

        {paymentMethods.length === 0 ? (
          <p className={styles.emptyState}>{text.noPaymentMethodsConfigured}</p>
        ) : (
          <form className={styles.form} onSubmit={handleSubmit}>
            {errorMsg ? <div className={styles.errorBanner}>{errorMsg}</div> : null}

            <div className={styles.field}>
              <span className={styles.label}>{text.paymentMethodLabel}</span>
              <div className={styles.paymentMethodsList}>
                {paymentMethods.map((m, idx) => {
                  const isSelected = selectedMethodIdx === idx;
                  const kindLabel =
                    m.kind === "instapay"
                      ? text.paymentMethodInstapay
                      : text.paymentMethodWallet;
                  return (
                    <label
                      key={`${m.kind}-${m.number_or_address}-${idx}`}
                      className={`${styles.paymentMethodCard} ${isSelected ? styles.paymentMethodCardSelected : ""}`}
                    >
                      <input
                        type="radio"
                        name="payment_method"
                        checked={isSelected}
                        onChange={() => setSelectedMethodIdx(idx)}
                        className={styles.paymentMethodRadio}
                      />
                      <div className={styles.paymentMethodDetails}>
                        <span className={styles.paymentMethodLabel}>
                          {kindLabel} {m.label ? `(${m.label})` : ""}
                        </span>
                        <span className={styles.paymentMethodAddress} dir="ltr">
                          {m.number_or_address}
                        </span>
                      </div>
                    </label>
                  );
                })}
              </div>
            </div>

            <div className={styles.field}>
              <label htmlFor="transfer-ref" className={styles.label}>
                {text.transferReferenceLabel}
              </label>
              <input
                id="transfer-ref"
                type="text"
                required
                maxLength={100}
                value={transferRef}
                onChange={(e) => setTransferRef(e.target.value)}
                placeholder={text.transferReferencePlaceholder}
                className={styles.input}
                dir="ltr"
              />
            </div>

            <div className={styles.field}>
              <label htmlFor="sender-number" className={styles.label}>
                {text.senderNumberLabel}
              </label>
              <input
                id="sender-number"
                type="text"
                required
                maxLength={50}
                value={senderNumber}
                onChange={(e) => setSenderNumber(e.target.value)}
                placeholder={text.senderNumberPlaceholder}
                className={styles.input}
                dir="ltr"
              />
            </div>

            <div className={styles.field}>
              <label htmlFor="amount" className={styles.label}>
                {text.amountLabel}
              </label>
              <input
                id="amount"
                type="number"
                required
                min={1}
                value={amount}
                onChange={(e) => setAmount(Number(e.target.value))}
                className={styles.input}
                dir="ltr"
              />
            </div>

            <button type="submit" disabled={submitting} className={styles.submit}>
              {submitting ? text.submittingPurchaseRequest : text.submitPurchaseRequest}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
