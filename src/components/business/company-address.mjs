// The public disclosure is rendered only on its required company-information page.
// Keep the residential address out of source and avoid echoing it in diagnostics.
export function companyAddress(value, enabled) {
  if (!enabled) return [];
  if (typeof value !== 'string') throw new Error('Company publication requires COMPANY_POSTAL_ADDRESS');
  const normalized = value.replace(/\r\n/g, '\n');
  const hasControls = /[\p{Cc}\p{Cf}]/u.test(normalized.replace(/\n/g, ''));
  const lines = normalized.trim().split('\n').map((line) => line.trim());
  if (hasControls || lines.length !== 2 || lines.some((line) => !line || line.length > 160)) {
    throw new Error('COMPANY_POSTAL_ADDRESS must contain two non-empty address lines');
  }
  return lines;
}
