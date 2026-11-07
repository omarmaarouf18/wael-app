"use client";

import { usePathname } from "next/navigation";
import { useEffect, useRef, type ReactNode } from "react";

import styles from "./site-shell.module.css";

/**
 * The header's menu under 1024 px: a native <details> disclosure (works
 * without JavaScript), closed again after a client-side navigation and on
 * Escape. From 1024 px it is hidden and the inline navigation shows instead.
 */
export function HeaderMenu({ label, children }: { label: string; children: ReactNode }) {
  const ref = useRef<HTMLDetailsElement>(null);
  const pathname = usePathname();

  useEffect(() => {
    if (ref.current) ref.current.open = false;
  }, [pathname]);

  return (
    <details
      ref={ref}
      className={styles.menu}
      onKeyDown={(e) => {
        if (e.key === "Escape" && ref.current?.open) {
          ref.current.open = false;
          ref.current.querySelector("summary")?.focus();
        }
      }}
    >
      <summary className={styles.menuButton}>
        <svg className={styles.menuIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
          <path d="M4 7h16M4 12h16M4 17h16" />
        </svg>
        <span className={styles.menuLabel}>{label}</span>
      </summary>
      <div className={styles.menuPanel}>{children}</div>
    </details>
  );
}
