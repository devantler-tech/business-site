import test from "node:test";
import assert from "node:assert/strict";
import * as journal from "../../src/data/journal.mjs";

// Catch dropped/misfiltered posts, profile URLs confused with author archives,
// and topic/author views that truncate the unpaginated published collection.
test("topic and author archives retain all actual members in source order", () => {
  assert.equal(typeof journal.journalArchives, "function", "the authored inventory must supply topic and author archives");
  const posts = Array.from({length:7}, (_, index) => ({
    title:`Post ${index}`, href:`https://devantler.tech/blog/post-${index}/`,
    tags:[{label:index % 2 ? "go" : "Go",href:"https://devantler.tech/blog/tags/go/"},
      ...(index === 0 ? [{label:"Web",href:"https://devantler.tech/blog/tags/web/"}] : [])],
    authors:index === 0 ? [{name:"Second author",url:"https://example.com/profile"}, {name:"Nikolai Emil Damm",url:"https://github.com/devantler"}] : [{name:"Nikolai Emil Damm",url:"https://github.com/devantler"}],
  }));
  const archives = journal.journalArchives(posts);
  assert.deepEqual(archives.map(archive => [archive.path, archive.posts.map(post => post.title)]), [
    ["/blog/tags/go/", ["Post 0","Post 1","Post 2","Post 3","Post 4","Post 5","Post 6"]],
    ["/blog/tags/web/", ["Post 0"]],
    ["/blog/authors/second-author/", ["Post 0"]],
    ["/blog/authors/nikolai-emil-damm/", ["Post 0","Post 1","Post 2","Post 3","Post 4","Post 5","Post 6"]],
  ]);
  assert.equal(archives[0].posts.length, 7, "topic archives must not silently retain only the first main-archive page");
  assert.equal(archives[3].posts.length, 7);
});

test("archive entries include every current topic and author page", () => {
  assert.equal(typeof journal.journalArchives, "function");
  const posts = journal.readJournal();
  const archives = journal.journalArchives(posts);
  assert.ok(archives.length > 1);
  for (const archive of archives) {
    assert.ok(archive.posts.length);
    assert.equal(new Set(archive.posts.map(post => post.href)).size, archive.posts.length);
    assert.ok(journal.journalPaths().includes(archive.path), `missing archive entry ${archive.path}`);
  }
});

test("slug-equivalent memberships never duplicate a post within an archive", () => {
  const author = {name:"Nikolai Emil Damm"};
  const posts = [
    {title:"Both labels", tags:[{label:"Go",href:"/blog/tags/go/"},{label:"go",href:"/blog/tags/go/"}], authors:[author,author]},
    {title:"Another post", tags:[{label:"go",href:"/blog/tags/go/"}], authors:[author]},
  ];
  assert.deepEqual(journal.journalArchives(posts).map(archive => [archive.path, archive.label, archive.posts.map(post => post.title)]), [
    ["/blog/tags/go/", "Go", ["Both labels", "Another post"]],
    ["/blog/authors/nikolai-emil-damm/", "Nikolai Emil Damm", ["Both labels", "Another post"]],
  ]);
});

test("deduplication cannot hide conflicting author names at the same slug", () => {
  assert.throws(() => journal.journalArchives([{tags:[],authors:[{name:"Ada"},{name:"ada"}]}]), /conflicting author archive slug/);
});
