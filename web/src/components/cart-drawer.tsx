"use client";

import { useEffect, useState } from "react";

import {
  ApiError,
  createOrder,
  getPaymentMethods,
  type PaymentMethod,
} from "@/lib/api";
import { dictionary, localePath, type Lang } from "@/lib/i18n";

import { useAuth } from "./auth-provider";
import { useCart } from "./cart-provider";
import styles from "./cart.module.css";

interface CartDrawerProps {
  lang: Lang;
}

export function CartDrawer({ lang }: CartDrawerProps) {
  const t = dictionary(lang);
  const cartText = t.cart;
  const checkoutText = t.checkout;
  const { isLoggedIn } = useAuth();
  const { items, totalPrice, itemCount, isCartOpen, closeCart, removeItem, clearCart } = useCart();

  const [inCheckout, setInCheckout] = useState(false);
  const [paymentMethods, setPaymentMethods] = useState<PaymentMethod[]>([]);
  const [selectedMethod, setSelectedMethod] = useState<"wallet" | "instapay" | "">("");
  const [transferRef, setTransferRef] = useState("");
  const [senderNumber, setSenderNumber] = useState("");
  const [customAmount, setCustomAmount] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  const amountPaid = customAmount !== null ? customAmount : totalPrice;

  const handleClose = () => {
    setInCheckout(false);
    setError(null);
    setSuccess(false);
    setCustomAmount(null);
    closeCart();
  };

  useEffect(() => {
    if (!isCartOpen) return;

    // Load payment methods when cart opens
    let cancelled = false;
    getPaymentMethods()
      .then((res) => {
        if (cancelled) return;
        setPaymentMethods(res.items || []);
        if (res.items && res.items.length > 0 && !selectedMethod) {
          const first = res.items[0];
          if (first) {
            setSelectedMethod(first.kind);
          }
        }
      })
      .catch(() => {
        // Fallback
      });

    return () => {
      cancelled = true;
    };
  }, [isCartOpen, selectedMethod]);

  if (!isCartOpen) return null;

  const handleCheckoutSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (items.length === 0) return;

    if (!selectedMethod) {
      setError(checkoutText.selectMethod);
      return;
    }

    if (transferRef.trim().length < 3 || transferRef.trim().length > 100) {
      setError(checkoutText.errors.invalid_reference);
      return;
    }

    if (senderNumber.trim().length < 3 || senderNumber.trim().length > 50) {
      setError(checkoutText.errors.invalid_sender);
      return;
    }

    if (!amountPaid || amountPaid <= 0) {
      setError(checkoutText.errors.invalid_amount);
      return;
    }

    setLoading(true);

    try {
      await createOrder({
        subject_ids: items.map((i) => i.id),
        payment_method: selectedMethod,
        transfer_reference: transferRef.trim(),
        sender_number: senderNumber.trim(),
        amount_paid: amountPaid,
      });

      clearCart();
      setSuccess(true);
    } catch (err: unknown) {
      if (err instanceof ApiError) {
        if (err.messageAr && lang === "ar") {
          setError(err.messageAr);
        } else if (err.messageEn && lang === "en") {
          setError(err.messageEn);
        } else if (err.code === "duplicate_reference") {
          setError(checkoutText.errors.duplicate_reference);
        } else if (err.code === "already_owned") {
          setError(checkoutText.errors.already_owned(err.subjectId));
        } else if (err.code === "pending_request_exists") {
          setError(checkoutText.errors.pending_request_exists(err.subjectId));
        } else if (err.code === "subject_expired") {
          setError(checkoutText.errors.subject_expired(err.subjectId));
        } else if (err.code === "subject_not_found") {
          setError(checkoutText.errors.subject_not_found(err.subjectId));
        } else if (err.code === "invalid_amount") {
          setError(checkoutText.errors.invalid_amount);
        } else if (err.code === "too_many_requests") {
          setError(checkoutText.errors.too_many_requests);
        } else {
          setError(checkoutText.errors.generic);
        }
      } else {
        setError(checkoutText.errors.generic);
      }
    } finally {
      setLoading(false);
    }
  };

  const legalTermsUrl =
    lang === "en" ? "https://legal.elmetracademy.app/terms#en" : "https://legal.elmetracademy.app/terms";
  const legalPrivacyUrl =
    lang === "en" ? "https://legal.elmetracademy.app/privacy#en" : "https://legal.elmetracademy.app/privacy";

  return (
    <div className={styles.backdrop} onClick={handleClose} role="dialog" aria-modal="true">
      <div className={styles.drawer} onClick={(e) => e.stopPropagation()}>
        <div className={styles.drawerHeader}>
          <h2 className={styles.drawerTitle}>
            {cartText.title} {itemCount > 0 ? `(${itemCount})` : ""}
          </h2>
          <button
            type="button"
            onClick={handleClose}
            className={styles.closeButton}
            aria-label={cartText.closeCart}
          >
            ✕
          </button>
        </div>

        {success ? (
          <div className={styles.successCard}>
            <div className={styles.successIcon}>✓</div>
            <h3 className={styles.successTitle}>{checkoutText.successTitle}</h3>
            <p className={styles.successLead}>{checkoutText.successLead}</p>
            <a
              href={localePath(lang, "/account")}
              onClick={handleClose}
              className={styles.primaryButton}
            >
              {checkoutText.viewMyOrders} →
            </a>
          </div>
        ) : itemCount === 0 ? (
          <div className={styles.emptyCart}>
            <p>{cartText.empty}</p>
            <a
              href={localePath(lang, "/subjects")}
              onClick={handleClose}
              className={styles.primaryButton}
            >
              {cartText.browseCatalog}
            </a>
          </div>
        ) : (
          <>
            <ul className={styles.itemList}>
              {items.map((item) => (
                <li key={item.id} className={styles.item}>
                  <div className={styles.itemInfo}>
                    <span className={styles.itemTitle}>
                      {lang === "ar" ? item.titleAr : item.titleEn || item.titleAr}
                    </span>
                    <span className={styles.itemPrice}>{cartText.itemPrice(item.price)}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => removeItem(item.id)}
                    className={styles.removeButton}
                  >
                    {cartText.remove}
                  </button>
                </li>
              ))}
            </ul>

            <div className={styles.cartSummary}>
              <div className={styles.totalRow}>
                <span>{lang === "ar" ? "المجموع الكلي:" : "Cart Total:"}</span>
                <span className={styles.totalAmount}>{cartText.itemPrice(totalPrice)}</span>
              </div>
            </div>

            {!inCheckout ? (
              <div className={styles.cartActions}>
                <button
                  type="button"
                  onClick={() => setInCheckout(true)}
                  className={styles.primaryButton}
                >
                  {cartText.proceedToCheckout}
                </button>
                <button
                  type="button"
                  onClick={clearCart}
                  className={styles.secondaryButton}
                >
                  {cartText.clearCart}
                </button>
              </div>
            ) : (
              <div className={styles.checkoutSection}>
                <h3 className={styles.checkoutTitle}>{checkoutText.title}</h3>

                {!isLoggedIn ? (
                  <div className={styles.emptyCart}>
                    <p>{checkoutText.loginRequiredPrompt}</p>
                    <div className={styles.buttonRowCentered}>
                      <a href={localePath(lang, "/login")} className={styles.primaryButton}>
                        {checkoutText.loginButton}
                      </a>
                      <a href={localePath(lang, "/signup")} className={styles.secondaryButton}>
                        {checkoutText.signupButton}
                      </a>
                    </div>
                  </div>
                ) : (
                  <form onSubmit={handleCheckoutSubmit} className={styles.checkoutForm}>
                    {error ? <div className={styles.errorBanner}>{error}</div> : null}

                    {amountPaid !== totalPrice && amountPaid > 0 ? (
                      <div className={styles.warningBanner}>
                        {checkoutText.amountMismatchWarning}
                      </div>
                    ) : null}

                    <div className={styles.field}>
                      <label htmlFor="checkout-method" className={styles.label}>
                        {checkoutText.paymentMethodLabel}
                      </label>
                      <select
                        id="checkout-method"
                        value={selectedMethod}
                        onChange={(e) =>
                          setSelectedMethod(e.target.value as "wallet" | "instapay")
                        }
                        required
                        className={styles.select}
                      >
                        <option value="">{checkoutText.selectMethod}</option>
                        {paymentMethods.map((pm, idx) => (
                          <option key={idx} value={pm.kind}>
                            {pm.label} ({pm.kind === "wallet" ? "محفظة كاش" : "انستاباي"}:{" "}
                            {pm.number_or_address})
                          </option>
                        ))}
                      </select>
                    </div>

                    <div className={styles.field}>
                      <label htmlFor="checkout-ref" className={styles.label}>
                        {checkoutText.transferReferenceLabel}
                      </label>
                      <input
                        id="checkout-ref"
                        type="text"
                        required
                        minLength={3}
                        maxLength={100}
                        placeholder={checkoutText.transferReferencePlaceholder}
                        value={transferRef}
                        onChange={(e) => setTransferRef(e.target.value)}
                        className={styles.input}
                      />
                    </div>

                    <div className={styles.field}>
                      <label htmlFor="checkout-sender" className={styles.label}>
                        {checkoutText.senderNumberLabel}
                      </label>
                      <input
                        id="checkout-sender"
                        type="text"
                        required
                        minLength={3}
                        maxLength={50}
                        placeholder={checkoutText.senderNumberPlaceholder}
                        value={senderNumber}
                        onChange={(e) => setSenderNumber(e.target.value)}
                        className={styles.input}
                      />
                    </div>

                    <div className={styles.field}>
                      <label htmlFor="checkout-amount" className={styles.label}>
                        {checkoutText.amountPaidLabel}
                      </label>
                      <input
                        id="checkout-amount"
                        type="number"
                        required
                        min={1}
                        value={amountPaid}
                        onChange={(e) => setCustomAmount(parseInt(e.target.value, 10) || 0)}
                        className={styles.input}
                      />
                    </div>

                    <p className={styles.legalNotice}>
                      {checkoutText.legalNoticePrefix}
                      <a href={legalTermsUrl} target="_blank" rel="noopener noreferrer">
                        {checkoutText.termsLink}
                      </a>
                      {checkoutText.legalNoticeAnd}
                      <a href={legalPrivacyUrl} target="_blank" rel="noopener noreferrer">
                        {checkoutText.privacyLink}
                      </a>
                      .
                    </p>

                    <button
                      type="submit"
                      disabled={loading}
                      className={styles.primaryButton}
                    >
                      {loading ? checkoutText.submitting : checkoutText.submitOrder}
                    </button>

                    <button
                      type="button"
                      onClick={() => setInCheckout(false)}
                      className={styles.secondaryButton}
                    >
                      {lang === "ar" ? "← العودة للسلة" : "← Back to Cart"}
                    </button>
                  </form>
                )}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
