import { parse } from 'parse5';

// Extract assertion text, not HTML for rendering. Parsing respects raw-text
// elements, case-insensitive tags, quoted attributes and character references.
export function readerText(html) {
  const text = node => {
    if (node.nodeName === '#text') return node.value;
    if (['script', 'style', 'template'].includes(node.tagName)) return '';
    return (node.childNodes ?? []).map(text).join(' ');
  };
  return text(parse(html)).trim().replace(/\s+/g, ' ');
}
