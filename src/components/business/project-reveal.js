
  const revealTarget = () => {
    let id;
    try { id = decodeURIComponent(location.hash.slice(1)); } catch { return; }
    if (!id) return;
    const target = document.getElementById(id);
    if (!target) return;
    const details = target.closest('details');
    if (details) { details.open = true; target.scrollIntoView(); }
  };
  revealTarget();
  window.addEventListener('hashchange', revealTarget);
