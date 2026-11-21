export type ThemeMode = "dark" | "light";
export type ProseFontMode = "serif" | "sans";

export interface NibPreferences {
  theme: ThemeMode;
  proseFont: ProseFontMode;
  proseIndent: boolean;
  proseFontSize: number;
  proseLineHeight: number;
  proseMaxWidth: number;
  studioDrawerOpen: boolean;
  lastBookId: number | null;
  lastChapterId: number | null;
}

const STORAGE_KEY = "nib.preferences.v1";

export const DEFAULT_TYPOGRAPHY = {
  proseFontSize: 17,
  proseLineHeight: 1.88,
  proseMaxWidth: 720,
} as const;

const DEFAULT_PREFERENCES: NibPreferences = {
  theme: "dark",
  proseFont: "serif",
  proseIndent: false,
  proseFontSize: DEFAULT_TYPOGRAPHY.proseFontSize,
  proseLineHeight: DEFAULT_TYPOGRAPHY.proseLineHeight,
  proseMaxWidth: DEFAULT_TYPOGRAPHY.proseMaxWidth,
  studioDrawerOpen: true,
  lastBookId: null,
  lastChapterId: null,
};

function clampNumber(value: unknown, min: number, max: number, fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) return fallback;
  return Math.min(max, Math.max(min, value));
}

export function loadPreferences(): NibPreferences {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULT_PREFERENCES };
    const parsed = JSON.parse(raw) as Partial<NibPreferences>;
    return {
      theme: parsed.theme === "light" ? "light" : "dark",
      proseFont: parsed.proseFont === "sans" ? "sans" : "serif",
      proseIndent: Boolean(parsed.proseIndent),
      proseFontSize: Math.round(clampNumber(parsed.proseFontSize, 14, 24, DEFAULT_TYPOGRAPHY.proseFontSize)),
      proseLineHeight:
        Math.round(clampNumber(parsed.proseLineHeight, 1.5, 2.3, DEFAULT_TYPOGRAPHY.proseLineHeight) * 100) /
        100,
      proseMaxWidth: Math.round(clampNumber(parsed.proseMaxWidth, 580, 960, DEFAULT_TYPOGRAPHY.proseMaxWidth)),
      studioDrawerOpen:
        typeof parsed.studioDrawerOpen === "boolean"
          ? parsed.studioDrawerOpen
          : true,
      lastBookId:
        typeof parsed.lastBookId === "number" && parsed.lastBookId > 0
          ? parsed.lastBookId
          : null,
      lastChapterId:
        typeof parsed.lastChapterId === "number" && parsed.lastChapterId > 0
          ? parsed.lastChapterId
          : null,
    };
  } catch {
    return { ...DEFAULT_PREFERENCES };
  }
}

export function savePreferences(prefs: NibPreferences): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs));
  } catch {
    // Ignore storage quota errors in restricted environments
  }
}

export function applyDocumentPreferences(
  prefs: Pick<
    NibPreferences,
    "theme" | "proseFont" | "proseIndent" | "proseFontSize" | "proseLineHeight" | "proseMaxWidth"
  >
): void {
  const root = document.documentElement;
  root.setAttribute("data-theme", prefs.theme);
  root.setAttribute("data-prose-font", prefs.proseFont);
  root.setAttribute("data-prose-indent", prefs.proseIndent ? "true" : "false");
  root.style.setProperty("--prose-font-size", `${prefs.proseFontSize}px`);
  root.style.setProperty("--prose-line-height", String(prefs.proseLineHeight));
  root.style.setProperty("--prose-max-width", `${prefs.proseMaxWidth}px`);
}
