import { afterEach, describe, expect, test } from "bun:test";
import { DEFAULT_TYPOGRAPHY, loadPreferences, savePreferences } from "./preferences";

const originalWindow = (globalThis as { window?: unknown }).window;

function mockWindowStorage(initial: Record<string, string> = {}) {
  const store = new Map<string, string>(Object.entries(initial));
  (globalThis as { window?: unknown }).window = {
    localStorage: {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => {
        store.set(k, v);
      },
    },
  };
  return store;
}

afterEach(() => {
  if (originalWindow === undefined) {
    delete (globalThis as { window?: unknown }).window;
  } else {
    (globalThis as { window?: unknown }).window = originalWindow;
  }
});

describe("preferences", () => {
  test("loads defaults when storage is empty or corrupted", () => {
    mockWindowStorage();
    const prefs = loadPreferences();
    expect(prefs.theme).toBe("dark");
    expect(prefs.proseFontSize).toBe(DEFAULT_TYPOGRAPHY.proseFontSize);

    mockWindowStorage({ "nib.preferences.v1": "{broken-json" });
    expect(loadPreferences().proseMaxWidth).toBe(DEFAULT_TYPOGRAPHY.proseMaxWidth);
  });

  test("clamps out-of-range typography numbers and persists round-trip", () => {
    mockWindowStorage({
      "nib.preferences.v1": JSON.stringify({
        theme: "light",
        proseFont: "sans",
        proseIndent: true,
        proseFontSize: 99,
        proseLineHeight: 0.5,
        proseMaxWidth: 2000,
        lastBookId: 7,
        lastChapterId: -3,
      }),
    });

    const loaded = loadPreferences();
    expect(loaded.theme).toBe("light");
    expect(loaded.proseFont).toBe("sans");
    expect(loaded.proseIndent).toBe(true);
    expect(loaded.proseFontSize).toBe(24);
    expect(loaded.proseLineHeight).toBe(1.5);
    expect(loaded.proseMaxWidth).toBe(960);
    expect(loaded.lastBookId).toBe(7);
    expect(loaded.lastChapterId).toBeNull();

    savePreferences(loaded);
    expect(loadPreferences()).toEqual(loaded);
  });
});
