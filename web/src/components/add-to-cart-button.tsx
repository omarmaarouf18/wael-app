"use client";

import { useState } from "react";
import { useCart } from "./cart-provider";
import type { Lang } from "@/lib/i18n";
import styles from "./add-to-cart-button.module.css";

interface AddToCartButtonProps {
  id: string;
  titleAr: string;
  titleEn: string;
  price: number;
  lang: Lang;
}

export function AddToCartButton({ id, titleAr, titleEn, price, lang }: AddToCartButtonProps) {
  const { addItem, isInCart } = useCart();
  const [justAdded, setJustAdded] = useState(false);
  const alreadyIn = isInCart(id);

  const handleAdd = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (alreadyIn) return;
    const toastText =
      lang === "ar" ? `تمت إضافة "${titleAr}" إلى السلة` : `Added "${titleEn || titleAr}" to cart`;
    const added = addItem({ id, titleAr, titleEn, price }, toastText);
    if (added) {
      setJustAdded(true);
      setTimeout(() => setJustAdded(false), 2000);
    }
  };

  if (alreadyIn || justAdded) {
    return (
      <span className={styles.inCartBadge} role="status">
        <svg className={styles.checkIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
          <polyline points="20 6 9 17 4 12" />
        </svg>
        <span>{lang === "ar" ? "في السلة" : "In cart"}</span>
      </span>
    );
  }

  return (
    <button
      type="button"
      onClick={handleAdd}
      className={styles.button}
      aria-label={lang === "ar" ? `أضف ${titleAr} إلى السلة` : `Add ${titleEn || titleAr} to cart`}
    >
      <svg className={styles.plusIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <line x1="12" y1="5" x2="12" y2="19" />
        <line x1="5" y1="12" x2="19" y2="12" />
      </svg>
      <span>{lang === "ar" ? "أضف للسلة" : "Add to cart"}</span>
    </button>
  );
}
