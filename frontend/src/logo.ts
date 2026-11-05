// The dbro mark, drawn with theme colors. Themes may set logo_body, logo_lid,
// logo_band, logo_glint and logo_ink; otherwise everything follows accent.
export function logo(size = 88, cls = 'logo'): SVGElement {
  const body = 'var(--logo-body, var(--accent))';
  const lid = 'var(--logo-lid, color-mix(in srgb, var(--logo-body, var(--accent)) 62%, white))';
  const band = 'var(--logo-band, color-mix(in srgb, var(--logo-body, var(--accent)) 62%, black))';
  const glint = 'var(--logo-glint, var(--yellow))';
  const ink = 'var(--logo-ink, var(--darker-background))';
  const wrap = document.createElement('span');
  wrap.innerHTML = `<svg class="${cls}" width="${size}" height="${size}" viewBox="90 96 332 344" aria-hidden="true">
  <path d="M100 150 V384 A156 48 0 0 0 412 384 V150 Z" style="fill:${body}"/>
  <path d="M100 311 A156 48 0 0 0 412 311 V325 A156 48 0 0 1 100 325 Z" style="fill:${band}"/>
  <ellipse cx="256" cy="150" rx="156" ry="48" style="fill:${lid}"/>
  <ellipse cx="256" cy="150" rx="156" ry="48" fill="none" stroke-width="10" opacity=".5" style="stroke:${band}"/>
  <path d="M114 204 H398 V224 H114 Z" style="fill:${ink}"/>
  <path d="M130 212 H246 V246 C246 268 230 280 206 280 H170 C146 280 130 266 130 244 Z" style="fill:${ink}"/>
  <path d="M266 212 H382 V244 C382 266 366 280 342 280 H306 C282 280 266 268 266 246 Z" style="fill:${ink}"/>
  <path d="M148 262 L160 262 L184 226 L172 226 Z M168 262 L175 262 L192 236 L185 236 Z" style="fill:${glint}"/>
  <path d="M284 262 L296 262 L320 226 L308 226 Z M304 262 L311 262 L328 236 L321 236 Z" style="fill:${glint}"/>
  <path d="M222 292 Q262 312 298 284" fill="none" stroke-width="15" stroke-linecap="round" style="stroke:${ink}"/>
</svg>`;
  return wrap.firstChild as SVGElement;
}
