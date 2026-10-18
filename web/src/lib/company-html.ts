/** Complete the two text-only slots, without ever substituting inside inserted data. */
export function companyHtml(html: string, lines: string[]): string {
  if (lines.length !== 2 || [1,2].some(index => html.split(`__BUSINESS_ADDRESS_LINE_${index}__`).length !== 2)) {
    throw new Error("Company HTML requires exactly two address slots");
  }
  return html.replace(/__BUSINESS_ADDRESS_LINE_([12])__/g, (_, index: string) =>
    lines[Number(index) - 1].replace(/[&<>"']/g, char => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"})[char]!));
}
