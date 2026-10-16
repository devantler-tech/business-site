/**
 * Copy the renderer's actual payload and report the clipboard outcome.
 * @param {{dataset:{code?:string},disabled:boolean,parentElement:{querySelector:(selector:string)=>{textContent:string|null}|null}|null}} button
 * @param {{writeText:(text:string)=>Promise<void>}|undefined} clipboard
 */
export async function copyJournalCode(button, clipboard = globalThis.navigator?.clipboard) {
  const status = button.parentElement?.querySelector("[aria-live]");
  if (!status) return;
  button.disabled = true;
  status.textContent = "";
  try {
    if (!clipboard || !button.dataset.code) throw new Error("Clipboard unavailable");
    await clipboard.writeText(button.dataset.code.replace(/\u007f/g, "\n"));
    status.textContent = "Copied!";
  } catch { status.textContent = "Copy failed. Select the code to copy it."; }
  finally { button.disabled = false; }
}

if (typeof document !== "undefined") {
  for (const button of document.querySelectorAll(".expressive-code .copy button")) {
    if (!(button instanceof HTMLButtonElement)) continue;
    button.addEventListener("click", event => { event.preventDefault(); void copyJournalCode(button); });
  }
  const update = () => {
    for (const pre of document.querySelectorAll(".expressive-code pre")) {
      if (!(pre instanceof HTMLElement)) continue;
      if (pre.scrollWidth > pre.clientWidth) {
        pre.tabIndex = 0;
        pre.setAttribute("role", "region");
        pre.setAttribute("aria-label", "Scrollable code block");
      } else {
        pre.removeAttribute("tabindex"); pre.removeAttribute("role"); pre.removeAttribute("aria-label");
      }
    }
  };
  update();
  new ResizeObserver(update).observe(document.documentElement);
}
