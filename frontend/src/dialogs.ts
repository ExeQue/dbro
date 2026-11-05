// Modal dialogs, context menus and toasts.
import { h, clear, icon } from './util';

export interface Button {
  label: string;
  primary?: boolean;
  danger?: boolean;
  /** Return false to keep the dialog open. */
  action?: () => unknown | Promise<unknown>;
}

let openCount = 0;

/** modal shows a dialog and resolves with the index of the pressed button (-1 for Escape). */
export function modal(title: string, body: Node | string, buttons: Button[] = [{ label: 'Close', primary: true }], opts: { wide?: boolean } = {}): Promise<number> {
  return new Promise((resolve) => {
    const errorBox = h('div', { class: 'dialog-error', hidden: true });
    const close = (i: number) => {
      backdrop.remove();
      openCount--;
      document.removeEventListener('keydown', onKey, true);
      resolve(i);
    };
    const btns = buttons.map((b, i) =>
      h('button', {
        type: 'button',
        class: 'btn' + (b.primary ? ' primary' : '') + (b.danger ? ' danger' : ''),
        onclick: async (e: Event) => {
          const el = e.currentTarget as HTMLButtonElement;
          if (b.action) {
            el.disabled = true;
            errorBox.hidden = true;
            try {
              const r = await b.action();
              if (r === false) return;
            } catch (err) {
              errorBox.textContent = errorMessage(err);
              errorBox.hidden = false;
              return;
            } finally {
              el.disabled = false;
            }
          }
          close(i);
        },
      }, b.label),
    );
    const dialog = h('div', { class: 'dialog' + (opts.wide ? ' wide' : ''), role: 'dialog', 'aria-modal': 'true', 'aria-label': title },
      h('div', { class: 'dialog-title' }, title),
      h('div', { class: 'dialog-body' }, body),
      errorBox,
      h('div', { class: 'dialog-buttons' }, btns),
    );
    const backdrop = h('div', { class: 'backdrop' }, dialog);
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        close(-1);
      }
      if (e.key === 'Enter' && !(e.target instanceof HTMLTextAreaElement) && !(e.target as HTMLElement).closest('.cm-editor')) {
        const primary = btns[buttons.findIndex((b) => b.primary)];
        if (primary) {
          e.preventDefault();
          primary.click();
        }
      }
    };
    document.addEventListener('keydown', onKey, true);
    document.body.appendChild(backdrop);
    openCount++;
    const first = dialog.querySelector<HTMLElement>('input:not([type=checkbox]), select, textarea') ?? btns.find((_, i) => buttons[i].primary);
    setTimeout(() => first?.focus(), 0);
  });
}

export const dialogOpen = () => openCount > 0;

export async function confirm(title: string, message: string, ok = 'OK', danger = false): Promise<boolean> {
  return (await modal(title, h('p', {}, message), [{ label: 'Cancel' }, { label: ok, primary: true, danger }])) === 1;
}

export async function prompt(title: string, label: string, value = '', type = 'text'): Promise<string | null> {
  const input = h('input', { type, value, class: 'input', 'aria-label': label });
  const r = await modal(title, h('label', { class: 'field' }, h('span', {}, label), input), [{ label: 'Cancel' }, { label: 'OK', primary: true }]);
  return r === 1 ? input.value : null;
}

export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

/** showError shows an error in a dialog. */
export function showError(err: unknown, title = 'Error') {
  return modal(title, h('pre', { class: 'error-text' }, errorMessage(err)));
}

// ---- toasts ----

let toastBox: HTMLElement | null = null;
export function toast(message: string, kind: 'info' | 'error' = 'info') {
  if (!toastBox) {
    toastBox = h('div', { class: 'toasts', role: 'status', 'aria-live': 'polite' });
    document.body.appendChild(toastBox);
  }
  const t = h('div', { class: 'toast ' + kind }, message);
  toastBox.appendChild(t);
  setTimeout(() => t.remove(), kind === 'error' ? 8000 : 4000);
}

// ---- context menu ----

export interface MenuItem {
  label: string;
  action?: () => void;
  danger?: boolean;
  disabled?: boolean;
  separator?: boolean;
  shortcut?: string;
  items?: MenuItem[];
}

let currentMenu: HTMLElement | null = null;

export function closeMenu() {
  currentMenu?.remove();
  currentMenu = null;
}

/** menu shows a context menu at x, y. */
export function menu(x: number, y: number, items: MenuItem[], parent?: HTMLElement): HTMLElement {
  if (!parent) closeMenu();
  const el = h('div', { class: 'menu', role: 'menu' });
  for (const it of items) {
    if (it.separator) {
      el.appendChild(h('div', { class: 'menu-sep', role: 'separator' }));
      continue;
    }
    const btn = h('button', {
      type: 'button', role: 'menuitem', class: 'menu-item' + (it.danger ? ' danger' : ''), disabled: it.disabled,
      onclick: () => {
        if (it.items) return;
        closeMenu();
        it.action?.();
      },
    }, h('span', {}, it.label), it.items ? icon('caretR', 12) : it.shortcut ? h('span', { class: 'shortcut' }, it.shortcut) : null);
    if (it.items) {
      let sub: HTMLElement | null = null;
      btn.addEventListener('mouseenter', () => {
        el.querySelectorAll('.menu.sub').forEach((s) => s.remove());
        const r = btn.getBoundingClientRect();
        sub = menu(r.right, r.top, it.items!, el);
        sub.classList.add('sub');
      });
    } else {
      btn.addEventListener('mouseenter', () => el.querySelectorAll(':scope > .menu.sub').forEach((s) => s.remove()));
    }
    el.appendChild(btn);
  }
  (parent ?? document.body).appendChild(el);
  if (parent) {
    el.style.position = 'fixed';
  }
  const r = el.getBoundingClientRect();
  el.style.left = Math.max(0, Math.min(x, window.innerWidth - r.width - 4)) + 'px';
  el.style.top = Math.max(0, Math.min(y, window.innerHeight - r.height - 4)) + 'px';
  if (!parent) {
    currentMenu = el;
    (el.querySelector('button:not([disabled])') as HTMLElement | null)?.focus();
  }
  return el;
}

document.addEventListener('mousedown', (e) => {
  if (currentMenu && !currentMenu.contains(e.target as Node)) closeMenu();
});
document.addEventListener('keydown', (e) => {
  if (!currentMenu) return;
  if (e.key === 'Escape') {
    closeMenu();
    e.stopPropagation();
  }
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    const btns = [...currentMenu.querySelectorAll<HTMLButtonElement>(':scope > button:not([disabled])')];
    const i = btns.indexOf(document.activeElement as HTMLButtonElement);
    btns[(i + (e.key === 'ArrowDown' ? 1 : -1) + btns.length) % btns.length]?.focus();
    e.preventDefault();
  }
}, true);

/** dropdown attaches a menu to a button. */
export function dropdown(label: Node | string, items: () => MenuItem[] | Promise<MenuItem[]>, cls = 'btn'): HTMLButtonElement {
  const b = h('button', { type: 'button', class: cls, 'aria-haspopup': 'menu' }, label, icon('caretD', 12));
  b.addEventListener('click', async () => {
    const r = b.getBoundingClientRect();
    menu(r.left, r.bottom + 2, await items());
  });
  return b;
}

/** field builds a labeled form row. */
export function field(label: string, control: HTMLElement, hint?: string): HTMLElement {
  return h('label', { class: 'field' }, h('span', {}, label), control, hint ? h('small', { class: 'hint' }, hint) : null);
}

export function setBusy(el: HTMLElement, busy: boolean) {
  el.classList.toggle('busy', busy);
}

export { clear };
