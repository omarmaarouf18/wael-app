"use client";

import { useEffect } from "react";

/**
 * Progressively enhances sections with data-reveal attribute to fade in
 * as they enter the viewport.
 * Users without JavaScript or with prefers-reduced-motion see all sections
 * normally with zero animation or delay.
 */
export function ScrollRevealInit() {
  useEffect(() => {
    if (typeof window === "undefined" || !("IntersectionObserver" in window)) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

    const sections = document.querySelectorAll<HTMLElement>("[data-reveal]");
    if (sections.length === 0) return;

    const observer = new IntersectionObserver(
      (entries, obs) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            entry.target.classList.add("is-revealed");
            obs.unobserve(entry.target);
          }
        }
      },
      { rootMargin: "0px 0px -40px 0px", threshold: 0.08 }
    );

    for (const section of sections) {
      section.classList.add("reveal-ready");
      observer.observe(section);
    }

    return () => observer.disconnect();
  }, []);

  return null;
}
