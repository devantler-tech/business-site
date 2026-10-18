import DOMPurify from "dompurify";

// Mermaid stays local and is loaded only by reading pages with authored diagrams.
const figures = [...document.querySelectorAll("figure.reading-diagram")];
let requested = 0;
let running = false;
const library = import("mermaid");

async function render() {
  requested++;
  if (running) return;
  running = true;
  try {
    const { default:mermaid } = await library;
    let completed = 0;
    while (completed !== requested) {
      completed = requested;
      const theme = document.documentElement.dataset.theme === "dark" ? "dark" : "default";
      mermaid.initialize({startOnLoad:false,securityLevel:"strict",theme,suppressErrorRendering:true});
      for (const figure of figures) {
        const canvas = figure.querySelector(".diagram-canvas");
        const status = figure.querySelector(".diagram-status");
        const source = figure.querySelector("details pre")?.textContent;
        if (!canvas || !status || !source) continue;
        canvas.setAttribute("aria-busy", "true");
        try {
          const { svg } = await mermaid.render(`diagram-${figure.getAttribute("data-diagram-id")}-${completed}`, source);
          // Theme changes can arrive while Mermaid is rendering. Never commit stale output.
          if (completed !== requested) break;
          // Retain Mermaid's HTML labels, with an explicit sanitizer at our DOM boundary.
          canvas.innerHTML = DOMPurify.sanitize(svg, {ADD_TAGS:["foreignobject"],ADD_ATTR:["dominant-baseline"],HTML_INTEGRATION_POINTS:{foreignobject:true}});
          const image = canvas.querySelector("svg");
          if (!image) throw new Error("Diagram output is missing");
          const dimensions = image.getAttribute("viewBox")?.trim().split(/\s+/).map(Number) ?? [];
          const [,,width,height] = dimensions;
          if (dimensions.length !== 4 || !dimensions.every(Number.isFinite) || width <= 0 || height <= 0) throw new Error("Diagram dimensions are missing");
          image.style.width = `${width}px`;
          image.setAttribute("role", "img");
          image.setAttribute("aria-label", figure.querySelector("figcaption")?.textContent ?? "Article diagram");
          status.textContent = "";
          figure.setAttribute("data-diagram-state", "rendered");
        } catch {
          canvas.textContent = "";
          status.textContent = "Diagram could not be displayed. Read its text below or reload this page.";
          figure.setAttribute("data-diagram-state", "error");
        } finally { canvas.setAttribute("aria-busy", "false"); }
      }
    }
  } catch {
    for (const figure of figures) {
      figure.querySelector(".diagram-canvas")?.setAttribute("aria-busy", "false");
      const status = figure.querySelector(".diagram-status");
      if (status) status.textContent = "Diagram could not be loaded. Read its text below or reload this page.";
      figure.setAttribute("data-diagram-state", "error");
    }
  } finally { running = false; }
}

new MutationObserver(() => { void render(); }).observe(document.documentElement, {attributes:true,attributeFilter:["data-theme"]});
void render();
