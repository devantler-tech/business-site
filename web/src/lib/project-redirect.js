const redirect = document.currentScript;
if (redirect?.dataset.destination && redirect.dataset.fallback) {
  location.replace(redirect.dataset.destination + (location.hash || redirect.dataset.fallback));
}
