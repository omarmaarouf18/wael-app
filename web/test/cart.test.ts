// Unit tests for Cart and local storage state persistence.
// Run: npm test

import assert from "node:assert/strict";
import { test } from "node:test";

interface CartItem {
  id: string;
  titleAr: string;
  titleEn: string;
  price: number;
}

const STORAGE_KEY = "elmetr_academy_cart_v1";

// In-memory mock localStorage
class MockLocalStorage {
  private store = new Map<string, string>();

  getItem(key: string): string | null {
    return this.store.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.store.set(key, value);
  }

  removeItem(key: string): void {
    this.store.delete(key);
  }

  clear(): void {
    this.store.clear();
  }
}

function loadSavedCart(storage: Storage): CartItem[] {
  try {
    const raw = storage.getItem(STORAGE_KEY);
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

function saveCart(storage: Storage, items: CartItem[]): void {
  try {
    storage.setItem(STORAGE_KEY, JSON.stringify(items));
  } catch {
    // Ignore storage quota or permission errors
  }
}

test("cart localStorage load and save resilience", () => {
  const storage = new MockLocalStorage() as unknown as Storage;

  // 1. Initial state is empty
  assert.deepEqual(loadSavedCart(storage), []);

  // 2. Corrupt JSON returns empty array without throwing
  storage.setItem(STORAGE_KEY, "{ not valid json");
  assert.deepEqual(loadSavedCart(storage), []);

  // 3. Non-array JSON returns empty array
  storage.setItem(STORAGE_KEY, JSON.stringify({ items: [] }));
  assert.deepEqual(loadSavedCart(storage), []);

  // 4. Filters out invalid objects
  storage.setItem(
    STORAGE_KEY,
    JSON.stringify([
      { id: "subj-1", titleAr: "مادة 1", titleEn: "Subj 1", price: 150 },
      { id: 123 }, // missing title and price
      null,
      "string-item",
    ]),
  );
  const loaded = loadSavedCart(storage);
  assert.equal(loaded.length, 1);
  assert.equal(loaded[0]?.id, "subj-1");

  // 5. Saving items persists correctly
  const items: CartItem[] = [
    { id: "s1", titleAr: "أ", titleEn: "A", price: 100 },
    { id: "s2", titleAr: "ب", titleEn: "B", price: 200 },
  ];
  saveCart(storage, items);
  assert.deepEqual(loadSavedCart(storage), items);
});

test("cart items calculation, addition, duplicate prevention and removal", () => {
  let cart: CartItem[] = [];

  const addItem = (item: CartItem): boolean => {
    if (cart.some((i) => i.id === item.id)) {
      return false;
    }
    cart = [...cart, item];
    return true;
  };

  const removeItem = (id: string): void => {
    cart = cart.filter((i) => i.id !== id);
  };

  const clearCart = (): void => {
    cart = [];
  };

  const getTotal = (): number => cart.reduce((sum, i) => sum + i.price, 0);

  // Add items
  assert.equal(addItem({ id: "s1", titleAr: "مادة 1", titleEn: "Subj 1", price: 150 }), true);
  assert.equal(cart.length, 1);
  assert.equal(getTotal(), 150);

  // Prevent duplicate
  assert.equal(addItem({ id: "s1", titleAr: "مادة 1", titleEn: "Subj 1", price: 150 }), false);
  assert.equal(cart.length, 1);

  // Add second item
  assert.equal(addItem({ id: "s2", titleAr: "مادة 2", titleEn: "Subj 2", price: 250 }), true);
  assert.equal(cart.length, 2);
  assert.equal(getTotal(), 400);

  // Remove item
  removeItem("s1");
  assert.equal(cart.length, 1);
  assert.equal(cart[0]?.id, "s2");
  assert.equal(getTotal(), 250);

  // Clear cart
  clearCart();
  assert.equal(cart.length, 0);
  assert.equal(getTotal(), 0);
});
