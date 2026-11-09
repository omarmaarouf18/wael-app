"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

import type { Lang } from "@/lib/i18n";

import styles from "./cart.module.css";

export interface CartItem {
  id: string;
  titleAr: string;
  titleEn: string;
  price: number;
}

export interface CartContextValue {
  items: CartItem[];
  itemCount: number;
  totalPrice: number;
  isCartOpen: boolean;
  toastMessage: string | null;
  addItem: (item: CartItem, toastText?: string) => boolean;
  removeItem: (id: string) => void;
  clearCart: () => void;
  isInCart: (id: string) => boolean;
  openCart: () => void;
  closeCart: () => void;
  toggleCart: () => void;
  showToast: (msg: string) => void;
}

const CartContext = createContext<CartContextValue | null>(null);

const STORAGE_KEY = "elmetr_academy_cart_v1";

function loadSavedCart(): CartItem[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (item): item is CartItem =>
        Boolean(item) &&
        typeof item === "object" &&
        typeof item.id === "string" &&
        typeof item.titleAr === "string" &&
        typeof item.price === "number",
    );
  } catch {
    return [];
  }
}

function saveCart(items: CartItem[]): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(items));
  } catch {
    // Ignore storage quota or permission errors
  }
}

export function CartProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<CartItem[]>([]);
  const [isCartOpen, setIsCartOpen] = useState(false);
  const [hydrated, setHydrated] = useState(false);
  const [toastMessage, setToastMessage] = useState<string | null>(null);

  useEffect(() => {
    const timer = setTimeout(() => {
      const saved = loadSavedCart();
      if (saved.length > 0) {
        setItems(saved);
      }
      setHydrated(true);
    }, 0);
    return () => clearTimeout(timer);
  }, []);

  useEffect(() => {
    if (!hydrated) return;
    saveCart(items);
  }, [items, hydrated]);

  const showToast = (msg: string) => {
    setToastMessage(msg);
  };

  useEffect(() => {
    if (!toastMessage) return;
    const timer = setTimeout(() => {
      setToastMessage(null);
    }, 2200);
    return () => clearTimeout(timer);
  }, [toastMessage]);

  const addItem = (item: CartItem, toastText?: string): boolean => {
    let added = false;
    setItems((prev) => {
      if (prev.some((i) => i.id === item.id)) {
        return prev;
      }
      added = true;
      return [...prev, item];
    });
    if (toastText) {
      showToast(toastText);
    }
    return added;
  };

  const removeItem = (id: string) => {
    setItems((prev) => prev.filter((i) => i.id !== id));
  };

  const clearCart = () => {
    setItems([]);
    try {
      window.localStorage.removeItem(STORAGE_KEY);
    } catch {
      // Ignore
    }
  };

  const isInCart = (id: string): boolean => {
    return items.some((i) => i.id === id);
  };

  const openCart = () => setIsCartOpen(true);
  const closeCart = () => setIsCartOpen(false);
  const toggleCart = () => setIsCartOpen((prev) => !prev);

  const totalPrice = items.reduce((sum, item) => sum + item.price, 0);

  return (
    <CartContext.Provider
      value={{
        items,
        itemCount: items.length,
        totalPrice,
        isCartOpen,
        toastMessage,
        addItem,
        removeItem,
        clearCart,
        isInCart,
        openCart,
        closeCart,
        toggleCart,
        showToast,
      }}
    >
      {children}
      {toastMessage ? (
        <div role="status" aria-live="polite" className={styles.cartToast}>
          <svg className={styles.toastIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <polyline points="20 6 9 17 4 12" />
          </svg>
          <span>{toastMessage}</span>
        </div>
      ) : null}
    </CartContext.Provider>
  );
}

export function useCart(): CartContextValue {
  const ctx = useContext(CartContext);
  if (!ctx) {
    throw new Error("useCart must be used within a CartProvider");
  }
  return ctx;
}

export function HeaderCartButton({ lang }: { lang: Lang }) {
  const { itemCount, openCart } = useCart();
  const [bumping, setBumping] = useState(false);

  useEffect(() => {
    if (itemCount === 0) return;
    const bumpTimer = setTimeout(() => setBumping(true), 0);
    const resetTimer = setTimeout(() => setBumping(false), 250);
    return () => {
      clearTimeout(bumpTimer);
      clearTimeout(resetTimer);
    };
  }, [itemCount]);

  return (
    <button
      type="button"
      onClick={openCart}
      className={styles.cartButton}
    >
      <svg className={styles.cartIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <path d="M3 4h2l2.4 11.2a1 1 0 0 0 1 .8h8.9a1 1 0 0 0 1-.8L20 8H6.2" />
        <circle cx="9.5" cy="19.5" r="1.3" />
        <circle cx="17" cy="19.5" r="1.3" />
      </svg>
      <span className={styles.cartLabel}>{lang === "ar" ? "السلة" : "Cart"}</span>
      {/* The visible words are the accessible name (WCAG 2.5.3): "السلة 2". */}
      {itemCount > 0 ? (
        <>
          {" "}
          <span className={`${styles.cartBadge} ${bumping ? styles.cartBadgeBump : ""}`}>
            {itemCount}
          </span>
        </>
      ) : null}
    </button>
  );
}
