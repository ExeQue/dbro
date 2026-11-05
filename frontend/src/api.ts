// Calls into Go, with the password and SSH host-key prompts handled in one place.
import * as Go from '../wailsjs/go/main/App';
import { h } from './util';
import { modal, field } from './dialogs';

export { Go };

/** call runs a Go call; when Go asks for a password or an SSH host key, it prompts and retries. */
export async function call<T>(f: () => Promise<T>, attempts = 3): Promise<T> {
  try {
    return await f();
  } catch (err) {
    const msg = typeof err === 'string' ? err : err instanceof Error ? err.message : String(err);
    if (attempts > 0 && msg.startsWith('NEED_PASSWORD:')) {
      const info = JSON.parse(msg.slice('NEED_PASSWORD:'.length)) as { conn: string; kind: string; message: string };
      if (await askPassword(info.conn, info.kind, info.message)) return call(f, attempts - 1);
      throw new Error('Canceled');
    }
    if (attempts > 0 && msg.startsWith('UNKNOWN_HOST:')) {
      const [host, fp] = msg.slice('UNKNOWN_HOST:'.length).split('|');
      const conn = guessConn(f);
      const ok = await modal('Unknown SSH host', h('div', {},
        h('p', {}, `The SSH server ${host} is not in ~/.ssh/known_hosts.`),
        h('p', {}, 'Fingerprint: ', h('code', {}, fp)),
        h('p', {}, 'Trust it only if this matches the fingerprint the server admin gave you.')),
      [{ label: 'Cancel' }, { label: 'Trust and connect', primary: true }]);
      if (ok === 1 && conn) {
        await Go.TrustHost(conn);
        return call(f, attempts - 1);
      }
      throw new Error('SSH host not trusted');
    }
    throw new Error(msg);
  }
}

// The connection name of the last call that hit an unknown host is tracked by the app;
// call sites that can trigger SSH set it here first.
let lastConn = '';
export function forConn<T>(conn: string, f: () => Promise<T>): Promise<T> {
  lastConn = conn;
  return call(f);
}
function guessConn(_f: unknown) {
  return lastConn;
}

async function askPassword(conn: string, kind: string, message: string): Promise<boolean> {
  const input = h('input', { type: 'password', class: 'input', autocomplete: 'off' });
  const remember = h('input', { type: 'checkbox', checked: true });
  const what = kind === 'ssh' ? 'SSH password or key passphrase' : 'Password';
  const r = await modal(`Connect to ${conn}`, h('div', { class: 'form' },
    message && !message.endsWith('needs a password') ? h('p', { class: 'muted' }, message) : null,
    field(what, input),
    h('label', { class: 'check' }, remember, ' Save in the system keyring'),
  ), [{ label: 'Cancel' }, {
    label: 'Connect', primary: true,
    action: () => Go.ProvidePassword(conn, kind, input.value, remember.checked).catch((e) => {
      const m = String(e);
      if (m.startsWith('NEED_PASSWORD:')) throw new Error(JSON.parse(m.slice(14)).message);
      throw new Error(m);
    }),
  }]);
  return r === 1;
}
