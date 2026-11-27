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

const directory = document.querySelector('[data-workspace-directory]');
const search = directory?.querySelector('[data-directory-search]');
if (search) {
  const query = directory.querySelector('input[type="search"]');
  const status = directory.querySelector('[data-directory-status]');
  const searchText = (value) => value.normalize('NFC').toLowerCase().trim().replace(/\s+/gu, ' ');
  const workspaces = Array.from(directory.querySelectorAll('.operator-workspace'), (element) => ({
    element,
    name: searchText(element.querySelector('summary').textContent),
    members: Array.from(element.querySelectorAll('.operator-member'), (member) => ({
      element: member,
      name: searchText(member.querySelector('.operator-person').textContent),
    })),
  }));
  let filtering = false;

  query.addEventListener('input', () => {
    const term = searchText(query.value);
    let hasResults = false;
    for (const workspace of workspaces) {
      if (term && !filtering) workspace.wasOpen = workspace.element.open;
      const nameMatches = workspace.name.includes(term);
      let memberMatches = false;
      for (const member of workspace.members) {
        const matches = member.name.includes(term);
        member.element.hidden = !nameMatches && !matches;
        memberMatches ||= matches;
      }
      workspace.element.hidden = !nameMatches && !memberMatches;
      if (!workspace.element.hidden) hasResults = true;
      if (term && !workspace.element.hidden) workspace.element.open = true;
      if (!term && filtering) workspace.element.open = workspace.wasOpen;
    }
    status.textContent = hasResults ? '' : status.dataset.noMatches;
    filtering = term !== '';
  });
  search.hidden = false;
}
