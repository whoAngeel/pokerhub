export const THEMES = {
  halloween: {
    label: 'Halloween',
    icon: '🎃',
    accent: '#dfab72',
    felt: '#514065',
    floor: '#302735',
    sign: 'THE HAUNTED CLUB',
  },
  classic: {
    label: 'Casino clásico',
    icon: '♠',
    accent: '#d9c58b',
    felt: '#2b6254',
    floor: '#223731',
    sign: 'THE ROYAL CLUB',
  },
  cyber: {
    label: 'Cyberpunk',
    icon: '✦',
    accent: '#89d9dd',
    felt: '#343c70',
    floor: '#22263c',
    sign: 'THE NEON CLUB',
  },
} as const;

export type ThemeId = keyof typeof THEMES;
export type Theme = (typeof THEMES)[ThemeId];
export const DEFAULT_THEME: ThemeId = 'halloween';
