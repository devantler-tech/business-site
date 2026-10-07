import type { APIRoute } from "astro";
import { cv } from "../../data/cv";
import { renderCvPdf } from "../../lib/cv-pdf";
import portrait from "../../assets/profile.jpg?inline";

/**
 * Renders the CV from src/data/cv.ts at build time and serves it as
 * /pdfs/nikolai-emil-damm-cv.pdf, so the download can never drift behind the deployed site.
 */
export const GET: APIRoute = async () => {
  const pdf = await renderCvPdf(cv, portrait);
  return new Response(new Uint8Array(pdf.buffer, pdf.byteOffset, pdf.byteLength), { headers: { "Content-Type": "application/pdf" } });
};
