const origin = 'https://widget.trustpilot.com';
const widgetId = 'devantler-review-collector';

// Start only the collector's frame protocol. The vendor bootstrap otherwise
// runs on the host page and also sends impression and visibility data.
/** @param {HTMLIFrameElement} frame */
export function connectReviewFrame(frame) {
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
    if (message?.widgetId === widgetId && message.command === 'ping') send({ command: 'pong' });
  }, options);

  return () => controller.abort();
}

const collector = globalThis.document?.querySelector('[data-review-collector]');
const toggle = collector?.querySelector('[data-review-toggle]');
const target = collector?.querySelector('#review-frame');
const template = collector?.querySelector('template[data-review-frame]');
const templateFrame = collector && template instanceof HTMLTemplateElement ? template.content.querySelector('iframe') : null;
const source = templateFrame?.dataset.src;
if (collector && toggle instanceof HTMLButtonElement && target instanceof HTMLElement && templateFrame && source) {
  const loadLabel = toggle.textContent?.trim() ?? '';
  const hideLabel = toggle.dataset.hideLabel ?? loadLabel;
  /** @type {(() => void) | undefined} */
  let disconnect;

  toggle.hidden = false;
  toggle.addEventListener('click', () => {
    const show = target.hidden;
    if (show) {
      const frame = document.importNode(templateFrame, true);
      disconnect = connectReviewFrame(frame);
      frame.src = source;
      target.append(frame);
    } else {
      disconnect?.();
      target.replaceChildren();
    }
    target.hidden = !show;
    toggle.setAttribute('aria-expanded', String(show));
    toggle.textContent = show ? hideLabel : loadLabel;
  });
}
