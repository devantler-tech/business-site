import { render } from "svelte/server";
import Contact from "../src/lib/private-contact/Contact.svelte";

/**
 * Render validated display input, never a request, principal or trusted HTML.
 * @param {import("../src/lib/private-contact/projection.js").ContactProjection} contact
 * @returns {string} Complete body for the caller's private Go frame.
 */
export function renderContact(contact) {
  try {
    const output = render(Contact, { props: { contact } });
    if (output.head !== "") throw new Error();
    return output.body;
  } catch {
    // Do not attach the original exception: it can contain a private projection.
    throw new Error("contact rendering unavailable");
  }
}
