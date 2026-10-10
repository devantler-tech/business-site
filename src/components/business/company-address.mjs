// The public disclosure is rendered only on its required company-information page.
// Keep the residential address out of source and avoid echoing it in diagnostics.
export function companyAddress(value, enabled) {
  if (!enabled) return [];
  if (typeof value !== 'string') throw new Error('Company publication requires COMPANY_POSTAL_ADDRESS');
  const lines = value.replace(/\r\n/g, '\n').trim().split('\n').map((line) => line.trim());
  if (lines.length !== 2 || lines.some((line) => !line || line.length > 160 || /[\p{Cc}\p{Cf}]/u.test(line))) {
    throw new Error('COMPANY_POSTAL_ADDRESS must contain two non-empty address lines');
  }
  return lines;
}
