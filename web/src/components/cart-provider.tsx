"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

import { dictionary, type Lang } from "@/lib/i18n";

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
  addItem: (item: CartItem) => boolean;
  removeItem: (id: string) => void;
  clearCart: () => void;
  isInCart: (id: string) => boolean;
  openCart: () => void;
  closeCart: () => void;
  toggleCart: () => void;
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

  const addItem = (item: CartItem): boolean => {
    let added = false;
    setItems((prev) => {
      if (prev.some((i) => i.id === item.id)) {
        return prev;
      }
      added = true;
      return [...prev, item];
    });
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
        addItem,
        removeItem,
        clearCart,
        isInCart,
        openCart,
        closeCart,
        toggleCart,
      }}
    >
      {children}
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
  const t = dictionary(lang);
  const { itemCount, openCart } = useCart();

  return (
    <button
      type="button"
      onClick={openCart}
      className={styles.cartButton}
      aria-label={t.cart.title}
    >
      <span>🛒</span>
      <span>{lang === "ar" ? "السلة" : "Cart"}</span>
      {itemCount > 0 ? <span className={styles.cartBadge}>{itemCount}</span> : null}
    </button>
  );
}
