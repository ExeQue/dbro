// Shared app state and a tiny event bus.
import type { config, db, main } from '../wailsjs/go/models';

export type Kind = 'connection' | 'database' | 'table' | 'view' | 'routine';

export interface Selection {
  kind: Kind;
  conn: string;
  db?: string;
  schema?: string;
  table?: string;
  routine?: db.Routine;
}

export const state = {
  boot: null as unknown as main.Bootstrap,
  connections: [] as config.Connection[],
  open: {} as Record<string, boolean>,
  selection: null as Selection | null,
};

export function connInfo(name: string): config.Connection | undefined {
  return state.connections.find((c) => c.name === name);
}

/** driverKind groups drivers by dialect. */
export function dialect(name: string): 'mysql' | 'postgres' | 'sqlite' {
  const d = connInfo(name)?.driver ?? 'mysql';
  if (d === 'mariadb' || d === 'mysql') return 'mysql';
  if (d.startsWith('postgres') || d === 'pgsql') return 'postgres';
  return 'sqlite';
}

export function quoteIdent(conn: string) {
  return dialect(conn) === 'mysql' ? (s: string) => '`' + s.replace(/`/g, '``') + '`' : (s: string) => '"' + s.replace(/"/g, '""') + '"';
}

type Handler = (data?: any) => void;
const handlers: Record<string, Handler[]> = {};

export function on(event: string, f: Handler) {
  (handlers[event] ??= []).push(f);
}

export function emit(event: string, data?: any) {
  for (const f of handlers[event] ?? []) f(data);
}
