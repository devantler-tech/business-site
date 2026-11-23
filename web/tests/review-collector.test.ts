import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";
import { connectReviewFrame, readReviewConsent, saveReviewConsent } from "../../server/internal/identity/web/review-collector.mjs";

test("optional review cookies require a valid, unexpired browser preference", () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
  };
  const now = 1_800_000_000_000;
  assert.equal(readReviewConsent(storage, now), null);
  saveReviewConsent(storage, "allow", now);
  assert.equal(readReviewConsent(storage, now + 1), "allow");
  saveReviewConsent(storage, "deny", now);
  assert.equal(readReviewConsent(storage, now + 1), "deny");
  assert.equal(readReviewConsent(storage, now + 180 * 86_400_000), null);
  saveReviewConsent(storage, "allow", now);
  const key = [...values.keys()][0];
  values.set(key, '{"decision":"allow","expiresAt":"never"}');
  assert.equal(readReviewConsent(storage, now), null);
  assert.equal(readReviewConsent(null, now), null);
});

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

test("the footer keeps the direct review link when consent is withdrawn before the frame is shown", () => {
  const stored = new Map<string, string>();
  const window = Object.assign(new EventTarget(), {
    localStorage: {
      getItem: (key: string) => stored.get(key) ?? null,
      setItem: (key: string, value: string) => stored.set(key, value),
    },
  });
  let focused: Element | null = null;
  class Element extends EventTarget {
    hidden = true;
    dataset: Record<string, string> = {};
    setAttribute() {}
    focus() { focused = this; }
  }
  class Anchor extends Element {}
  class Button extends Element {}
  class Template {
    content = { querySelector: () => ({ dataset: { src: "https://widget.trustpilot.com/example" } }) };
  }
  const link = new Anchor();
  const settings = new Button();
  const allow = new Button();
  const deny = new Button();
  allow.dataset.reviewDecision = "allow";
  deny.dataset.reviewDecision = "deny";
  const panel = Object.assign(new Element(), {
    querySelector: () => deny,
    contains: (element: unknown) => element === deny || element === allow,
  });
  function frame() {
    return Object.assign(new EventTarget(), {
      contentWindow: { postMessage() {} },
      ownerDocument: { defaultView: window },
      removeAttribute() {},
      src: "",
    });
  }
  const frames: ReturnType<typeof frame>[] = [];
  const container = Object.assign(new Element(), {
    append(next: ReturnType<typeof frame>) { frames.push(next); },
    contains: (element: unknown) => frames.some(frame => frame === element),
    replaceChildren() { frames.length = 0; },
  });
  const controls = new Map<string, Element | Template>([
    ["#review-title", link], ["[data-review-settings]", settings], ["[data-review-consent]", panel],
    ["#review-frame", container], ["template[data-review-frame]", new Template()],
  ]);
  const document = {
    documentElement: { dataset: { theme: "light" } },
    get activeElement() { return focused; },
    importNode: frame,
    querySelector: () => ({ querySelector: (selector: string) => controls.get(selector) }),
  };
  const script = readFileSync(new URL("../../server/internal/identity/web/review-collector.mjs", import.meta.url), "utf8");
  runInNewContext(script.replace(/^export /gm, ""), {
    document, window, HTMLElement: Element, HTMLAnchorElement: Anchor, HTMLButtonElement: Button,
    HTMLTemplateElement: Template, AbortController, URL, URLSearchParams,
  });
  const choose = (button: Button) => {
    button.focus();
    const event = new Event("click");
    Object.defineProperties(event, { target: { value: button }, detail: { value: 0 } });
    panel.dispatchEvent(event);
  };
  const ping = (active: ReturnType<typeof frame>) => {
    window.dispatchEvent(Object.assign(new Event("message"), {
      data: JSON.stringify({ command: "ping", widgetId: "devantler-review-collector" }),
      origin: "https://widget.trustpilot.com",
      source: active.contentWindow,
    }));
  };

  assert.equal(frames.length, 0);
  assert.equal(link.hidden, false);
  assert.equal(panel.hidden, false);
  choose(allow);
  assert.equal(focused, settings);
  assert.equal(frames.length, 1);
  assert.equal(new URL(frames[0].src).origin, "https://widget.trustpilot.com");
  link.focus();
  ping(frames[0]);
  assert.equal(link.hidden, false);
  assert.equal(container.dataset.ready, undefined);
  settings.dispatchEvent(new Event("click"));
  choose(deny);
  link.dispatchEvent(new Event("blur"));
  assert.equal(frames.length, 0);
  assert.equal(container.hidden, true);
  assert.equal(container.dataset.ready, undefined);
  assert.equal(link.hidden, false);
  assert.equal(focused, settings);
});
