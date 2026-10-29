import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";

/** Read this source file's committed modification date, never a build timestamp.
 * @param {string} path @param {string|URL} directory
 */
export function committedContentDate(path, directory = new URL("../../", import.meta.url)) {
  const env = {...process.env,GIT_NO_REPLACE_OBJECTS:"1",GIT_NO_LAZY_FETCH:"1"};
  for (const name of ["GIT_DIR","GIT_WORK_TREE","GIT_COMMON_DIR","GIT_INDEX_FILE","GIT_OBJECT_DIRECTORY","GIT_ALTERNATE_OBJECT_DIRECTORIES","GIT_PREFIX","GIT_NAMESPACE"]) delete env[name];
  const value = execFileSync("git",["--no-replace-objects","log","-1","--format=%cI","--",path],{cwd:directory,env,encoding:"utf8"}).trim();
  assert.ok(value && Number.isFinite(Date.parse(value)), "Committed content history is required");
  return new Date(value).toISOString();
}
