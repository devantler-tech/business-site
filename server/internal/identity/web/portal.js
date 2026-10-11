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
