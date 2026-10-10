import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readerText } from './reader-text.mjs';
test('reader text excludes scripts and styles regardless of HTML casing', () => {
  assert.equal(readerText('<main><p>A public website</p><SCRIPT>family</SCRIPT><STYLE>Wedding App</STYLE><p>Six tools</p></main>'), 'A public website Six tools');
});
test('reader text decodes entities and preserves nested ordinary text', () => {
  assert.equal(readerText('<p>Research &amp; <strong>completed work</strong></p><!-- family -->'), 'Research & completed work');
});
test('reader text is not fooled by tag-like text inside a script or an attribute', () => {
  assert.equal(readerText('<script>const x = "<script>family";</script><p title="family > hidden">Public work</p>'), 'Public work');
});
