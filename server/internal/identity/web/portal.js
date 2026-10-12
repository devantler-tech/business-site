const theme = document.querySelector('#theme');
if (theme) {
  const apply = (value) => {
    document.documentElement.dataset.theme = value;
  };
  let value = 'auto';
  try { value = localStorage.getItem('starlight-theme') || 'auto'; } catch {}
  if (!['auto', 'light', 'dark'].includes(value)) value = 'auto';
  theme.value = value;
  apply(value);
  theme.addEventListener('change', () => {
    apply(theme.value);
    try { localStorage.setItem('starlight-theme', theme.value); } catch {}
  });
}

// Count Unicode code points like Go and PostgreSQL, not UTF-16 storage units.
// The server remains authoritative, including when JavaScript is unavailable.
function requestCharacterError(value, limit, lang) {
  if (Array.from(value).length <= limit) return '';
  return lang === 'da' ? `Brug højst ${limit} tegn.` : `Use up to ${limit} characters.`;
}

for (const field of document.querySelectorAll('[data-character-limit]')) {
  const check = () => field.setCustomValidity(requestCharacterError(field.value, Number(field.dataset.characterLimit), document.documentElement.lang));
  field.addEventListener('input', check);
  check();
}
