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
