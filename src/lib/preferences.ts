export type ThemeMode = "dark" | "light";
export type ProseFontMode = "serif" | "sans";

export interface NibPreferences {
  theme: ThemeMode;
  proseFont: ProseFontMode;
  proseIndent: boolean;
  studioDrawerOpen: boolean;
  lastBookId: number | null;
  lastChapterId: number | null;
}

const STORAGE_KEY = "nib.preferences.v1";

const DEFAULT_PREFERENCES: NibPreferences = {
  theme: "dark",
  proseFont: "serif",
  proseIndent: false,
  studioDrawerOpen: true,
  lastBookId: null,
  lastChapterId: null,
};

export function loadPreferences(): NibPreferences {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULT_PREFERENCES };
    const parsed = JSON.parse(raw) as Partial<NibPreferences>;
    return {
      theme: parsed.theme === "light" ? "light" : "dark",
      proseFont: parsed.proseFont === "sans" ? "sans" : "serif",
      proseIndent: Boolean(parsed.proseIndent),
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

export function applyDocumentPreferences(prefs: Pick<NibPreferences, "theme" | "proseFont" | "proseIndent">): void {
  const root = document.documentElement;
  root.setAttribute("data-theme", prefs.theme);
  root.setAttribute("data-prose-font", prefs.proseFont);
  root.setAttribute("data-prose-indent", prefs.proseIndent ? "true" : "false");
}
