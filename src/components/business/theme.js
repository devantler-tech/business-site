// Inline in the head so the saved preference is applied before the first paint.
(() => {
  const key = 'starlight-theme';
  const parse = (value) => value === 'light' || value === 'dark' ? value : 'auto';
  const media = window.matchMedia('(prefers-color-scheme: light)');
  const root = document.documentElement;
  let preference = 'auto';
  let select;
  try { preference = parse(window.localStorage.getItem(key)); } catch { /* Storage may be disabled. */ }

  const apply = () => {
    const theme = preference === 'auto' ? (media.matches ? 'light' : 'dark') : preference;
    root.dataset.theme = theme;
    root.style.colorScheme = theme;
    if (select) select.value = preference;
  };
  apply();

  media.addEventListener('change', () => { if (preference === 'auto') apply(); });
  window.addEventListener('storage', (event) => {
    if (event.key !== key && event.key !== null) return;
    preference = parse(event.newValue);
    apply();
  });

  const bind = () => {
    select = document.getElementById('theme-select');
    const control = document.getElementById('theme-control');
    if (!select || !control) return;
    apply();
    select.addEventListener('change', () => {
      preference = parse(select.value);
      apply();
      // Starlight represents System with an empty value, not a literal "auto".
      try { window.localStorage.setItem(key, preference === 'auto' ? '' : preference); } catch { /* Keep the in-page choice. */ }
    });
    control.hidden = false;
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', bind, { once: true });
  else bind();
})();
