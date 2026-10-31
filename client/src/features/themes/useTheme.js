import { useEffect, useState } from 'react';
import { DEFAULT_THEME, THEMES } from './themes';

export function useTheme() {
  const [themeId, setThemeId] = useState(() => {
    try {
      const stored = localStorage.getItem('royale-theme');
      return Object.hasOwn(THEMES, stored) ? stored : DEFAULT_THEME;
    } catch {
      return DEFAULT_THEME;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem('royale-theme', themeId);
    } catch {
      /* Storage may be unavailable in private browsing. */
    }
  }, [themeId]);
  return { themeId, theme: THEMES[themeId], setThemeId };
}
