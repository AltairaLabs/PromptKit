// Inline Atlas icons. Thin-stroke, rounded, no CDN. star-filled = the one
// allowed filled glyph (gold). `cls` lets callers style stroke via CSS.
const STROKE =
  'fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"';

const P: Record<string, string> = {
  'graduation-cap': `<path ${STROKE} d="M22 10 12 5 2 10l10 5 10-5Z"/><path ${STROKE} d="M6 12v5c0 1 3 3 6 3s6-2 6-3v-5"/>`,
  'list-checks': `<path ${STROKE} d="m3 5 2 2 3-3"/><path ${STROKE} d="M11 6h10M11 12h10M11 18h10"/><path ${STROKE} d="m3 11 2 2 3-3M4 17v3h3"/>`,
  'book-open': `<path ${STROKE} d="M12 7v13M12 7C10.5 5.5 8 5 3 5v13c5 0 7.5.5 9 2 1.5-1.5 4-2 9-2V5c-5 0-7.5.5-9 2Z"/>`,
  'compass': `<circle ${STROKE} cx="12" cy="12" r="9"/><path ${STROKE} d="m15.5 8.5-2 5-5 2 2-5 5-2Z"/>`,
  'sun': `<circle ${STROKE} cx="12" cy="12" r="4"/><path ${STROKE} d="M12 2v2M12 20v2M4 12H2M22 12h-2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19"/>`,
  'moon': `<path ${STROKE} d="M21 12.8A8.5 8.5 0 1 1 11.2 3a6.6 6.6 0 0 0 9.8 9.8Z"/>`,
  'copy': `<rect ${STROKE} x="9" y="9" width="11" height="11" rx="2"/><path ${STROKE} d="M5 15V5a2 2 0 0 1 2-2h8"/>`,
  'arrow-right': `<path ${STROKE} d="M5 12h14M13 6l6 6-6 6"/>`,
  'external': `<path ${STROKE} d="M14 5h5v5M19 5l-8 8M12 5H7a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-5"/>`,
  'star-filled': `<path fill="currentColor" d="M12 2.5 14 9l6.5.4-5 4.2 1.7 6.4L12 16.6 6.8 20l1.7-6.4-5-4.2L10 9Z"/>`,

  // Capability + spine glyphs
  'grid': `<rect ${STROKE} x="3" y="3" width="7" height="7" rx="1.5"/><rect ${STROKE} x="14" y="3" width="7" height="7" rx="1.5"/><rect ${STROKE} x="3" y="14" width="7" height="7" rx="1.5"/><rect ${STROKE} x="14" y="14" width="7" height="7" rx="1.5"/>`,
  'activity': `<path ${STROKE} d="M3 12h4l2 6 4-14 2 8h6"/>`,
  'wrench': `<path ${STROKE} d="M14.7 6.3a4 4 0 0 1 0 5.6l-3 3a4 4 0 0 1-5.6-5.6l1.5-1.5"/><path ${STROKE} d="M9.3 17.7a4 4 0 0 1 0-5.6l3-3a4 4 0 0 1 5.6 5.6l-1.5 1.5"/>`,
  'network': `<circle ${STROKE} cx="6" cy="6" r="2.5"/><circle ${STROKE} cx="18" cy="6" r="2.5"/><circle ${STROKE} cx="12" cy="18" r="2.5"/><path ${STROKE} d="M7.6 7.8 11 15.5M16.4 7.8 13 15.5M8.5 6h7"/>`,
  'workflow': `<rect ${STROKE} x="3" y="4" width="6" height="6" rx="1.5"/><rect ${STROKE} x="15" y="14" width="6" height="6" rx="1.5"/><path ${STROKE} d="M6 10v3a2 2 0 0 0 2 2h7"/>`,
  'audio': `<path ${STROKE} d="M12 3v18M8 7v10M4 10v4M16 7v10M20 10v4"/>`,
  'image': `<rect ${STROKE} x="3" y="5" width="18" height="14" rx="2"/><circle ${STROKE} cx="9" cy="11" r="2"/><path ${STROKE} d="m4 18 4-3 3 2 4-4 5 4"/>`,
  'chart': `<path ${STROKE} d="M3 3v18h18"/><path ${STROKE} d="M7 14l3-4 3 3 4-6"/>`,
  'shield-check': `<path ${STROKE} d="M12 2 4 5v6c0 5 3.5 7.5 8 9 4.5-1.5 8-4 8-9V5Z"/><path ${STROKE} d="m9 12 2 2 4-4"/>`,
  'database': `<ellipse ${STROKE} cx="12" cy="6" rx="8" ry="3"/><path ${STROKE} d="M4 6v6c0 1.7 3.6 3 8 3s8-1.3 8-3V6"/><path ${STROKE} d="M4 12v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"/>`,
  'user': `<circle ${STROKE} cx="12" cy="8" r="4"/><path ${STROKE} d="M4 20c0-4 4-6 8-6s8 2 8 6"/>`,
  'cpu': `<rect ${STROKE} x="5" y="5" width="14" height="14" rx="2"/><rect ${STROKE} x="9" y="9" width="6" height="6" rx="1"/><path ${STROKE} d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"/>`,
};

export function icon(name: keyof typeof P | string, size = 20, cls = ''): string {
  return `<svg viewBox="0 0 24 24" width="${size}" height="${size}" class="${cls}" aria-hidden="true">${P[name] ?? ''}</svg>`;
}
