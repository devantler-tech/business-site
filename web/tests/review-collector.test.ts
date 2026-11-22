import assert from "node:assert/strict";
import test from "node:test";
import { connectReviewFrame } from "../../server/internal/identity/web/review-collector.mjs";

test("the collector talks only to its own Trustpilot frame and disconnects on hide", () => {
  const origin = "https://widget.trustpilot.com";
  const host = new EventTarget();
  const sent: [string, string][] = [];
  const contentWindow = { postMessage: (data: string, target: string) => sent.push([data, target]) };
  const frame = Object.assign(new EventTarget(), { contentWindow, ownerDocument: { defaultView: host } });
  const disconnect = connectReviewFrame(frame as unknown as HTMLIFrameElement);

  frame.dispatchEvent(new Event("load"));
  const init = JSON.parse(sent[0][0]);
  assert.deepEqual(sent, [[JSON.stringify({ command: "setId", widgetId: "devantler-review-collector" }), origin]]);

  const receive = (data: unknown, sender: unknown = contentWindow, sourceOrigin = origin) => {
    host.dispatchEvent(Object.assign(new Event("message"), { data, source: sender, origin: sourceOrigin }));
  };
  const ping = JSON.stringify({ command: "ping", widgetId: init.widgetId });
  receive(ping, {});
  receive(ping, contentWindow, "https://example.invalid");
  receive(JSON.stringify({ command: "ping", widgetId: "other-widget" }));
  receive(JSON.stringify({ command: "impression", widgetId: init.widgetId }));
  receive("invalid JSON");
  receive(null);
  assert.equal(sent.length, 1);

  receive(ping);
  assert.deepEqual(sent[1], [JSON.stringify({ command: "pong" }), origin]);
  disconnect();
  receive(ping);
  frame.dispatchEvent(new Event("load"));
  assert.equal(sent.length, 2);
});
