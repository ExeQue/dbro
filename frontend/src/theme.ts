// Applies a theme's Omarchy color keys as CSS variables.
import type { theme } from '../wailsjs/go/models';

let current: theme.Theme | null = null;

export function applyTheme(t: theme.Theme) {
  current = t;
  const root = document.documentElement;
  for (const [k, v] of Object.entries(t.colors)) {
    if (k !== 'mode') root.style.setProperty('--' + k.replace(/_/g, '-'), v);
  }
  root.dataset.mode = t.colors.mode === 'light' ? 'light' : 'dark';
  root.style.colorScheme = root.dataset.mode;
}

export const currentTheme = () => current;

export function applyFont(font: string) {
  document.documentElement.style.setProperty('--font', font ? `'${font}', monospace` : 'monospace');
}
