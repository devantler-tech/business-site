const origin = 'https://widget.trustpilot.com';
const widgetId = 'devantler-review-collector';
const consentKey = 'devantler:trustpilot-consent-v1';
const consentDuration = 180 * 86_400_000;

/** @typedef {'allow' | 'deny'} ReviewConsent */

/**
 * @param {Pick<Storage, 'getItem'> | null} storage
 * @returns {ReviewConsent | null}
 */
export function readReviewConsent(storage, now = Date.now()) {
  try {
    const entry = JSON.parse(storage?.getItem(consentKey) ?? 'null');
    if ((entry?.decision === 'allow' || entry?.decision === 'deny') &&
        Number.isSafeInteger(entry.expiresAt) && entry.expiresAt > now &&
        entry.expiresAt <= now + consentDuration) {
      return entry.decision;
    }
  } catch {
    // Missing or inaccessible storage leaves optional requests blocked.
  }
  return null;
}

/**
 * @param {Pick<Storage, 'setItem'> | null} storage
 * @param {ReviewConsent} decision
 */
export function saveReviewConsent(storage, decision, now = Date.now()) {
  try {
    storage?.setItem(consentKey, JSON.stringify({ decision, expiresAt: now + consentDuration }));
  } catch {
    // An explicit choice still applies for the lifetime of this page.
  }
}

// Use the frame protocol without loading the vendor bootstrap on our page.
/** @param {HTMLIFrameElement} frame @param {() => void} [onReady] */
export function connectReviewFrame(frame, onReady) {
  const controller = new AbortController();
  const options = { signal: controller.signal };
  /** @param {object} message */
  const send = message => frame.contentWindow?.postMessage(JSON.stringify(message), origin);

  frame.addEventListener('load', () => send({ command: 'setId', widgetId }), options);
  frame.ownerDocument.defaultView?.addEventListener('message', event => {
    if (event.origin !== origin || event.source !== frame.contentWindow || typeof event.data !== 'string') return;
    let message;
    try {
      message = JSON.parse(event.data);
    } catch {
      return;
    }
    if (message?.widgetId === widgetId && message.command === 'ping') {
      send({ command: 'pong' });
      onReady?.();
    }
  }, options);

  return () => controller.abort();
}

const collector = globalThis.document?.querySelector('[data-review-collector]');
const link = collector?.querySelector('#review-title');
const settings = collector?.querySelector('[data-review-settings]');
const panel = collector?.querySelector('[data-review-consent]');
const target = collector?.querySelector('#review-frame');
const template = collector?.querySelector('template[data-review-frame]');
const templateFrame = collector && template instanceof HTMLTemplateElement ? template.content.querySelector('iframe') : null;
const source = templateFrame?.dataset.src;

if (collector && link instanceof HTMLAnchorElement && settings instanceof HTMLButtonElement && panel instanceof HTMLElement &&
    target instanceof HTMLElement && templateFrame && source) {
  /** @type {Storage | null} */
  let storage = null;
  try {
    storage = window.localStorage;
  } catch {
    // The fallback link and the current visit's choice work without storage.
  }
  /** @type {(() => void) | undefined} */
  let disconnect;
  let openedFromSettings = false;

  const stopWidget = () => {
    const focused = target.contains(document.activeElement);
    disconnect?.();
    disconnect = undefined;
    target.replaceChildren();
    target.hidden = true;
    delete target.dataset.ready;
    link.hidden = false;
    if (focused) link.focus();
  };

  const startWidget = () => {
    if (disconnect) return;
    const frame = document.importNode(templateFrame, true);
    const url = new URL(source);
    const parameters = new URLSearchParams(url.hash.slice(1));
    const appearance = document.documentElement.dataset.theme;
    parameters.set('theme', appearance === 'light' || appearance === 'dark'
      ? appearance
      : window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    url.hash = parameters.toString();
    frame.tabIndex = -1;
    const showWidget = () => {
      if (!target.contains(frame)) return;
      target.dataset.ready = '';
      frame.removeAttribute('tabindex');
      link.hidden = true;
    };
    disconnect = connectReviewFrame(frame, () => {
      if (document.activeElement === link) {
        link.addEventListener('blur', showWidget, { once: true });
      } else {
        showWidget();
      }
    });
    frame.src = url.href;
    target.hidden = false;
    target.append(frame);
  };

  /** @param {boolean} open */
  const showPreferences = open => {
    panel.hidden = !open;
    settings.setAttribute('aria-expanded', String(open));
  };
  /** @param {ReviewConsent | null} decision */
  const applyConsent = decision => {
    if (decision === 'allow') startWidget();
    else stopWidget();
    showPreferences(decision === null);
  };

  settings.hidden = false;
  applyConsent(readReviewConsent(storage));
  settings.addEventListener('click', () => {
    openedFromSettings = panel.hidden === true;
    showPreferences(openedFromSettings);
    if (openedFromSettings) panel.querySelector('button')?.focus();
  });
  panel.addEventListener('click', event => {
    if (!(event.target instanceof HTMLButtonElement)) return;
    const decision = event.target.dataset.reviewDecision;
    if (decision !== 'allow' && decision !== 'deny') return;
    const returnFocus = openedFromSettings && panel.contains(document.activeElement);
    openedFromSettings = false;
    saveReviewConsent(storage, decision);
    applyConsent(decision);
    if (returnFocus) settings.focus();
  });
  window.addEventListener('storage', event => {
    if (event.key === consentKey || event.key === null) applyConsent(readReviewConsent(storage));
  });
}
