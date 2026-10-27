// Explicit fictional-fixture WebDriver journey; no ordinary build entrypoint.
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtemp, readFile, readlink, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const origin = 'http://fixture.localhost:8080';
let phase = 'admission';
let category = 'observation';

// Treat a browser observation as evidence only after the expected native page,
// loaded typography and resource origins have all been observed together.
export function admitPage(page, kind) {
  if (!page || !['login', 'password', 'console'].includes(kind)) return false;
  let url;
  try { url = new URL(page.url); } catch { return false; }
  if (url.origin !== origin || url.username || url.password || page.ready !== 'complete' || typeof page.text !== 'string' || !page.text.trim() ||
      typeof page.font !== 'string' || !page.font.trim() || /ailerons/i.test(page.font) || page.fontsReady !== true ||
      page.secureContext !== true || page.webCrypto !== true || page.selectedFaceLoaded !== true || !Array.isArray(page.failedFonts) || page.failedFonts.length || !Array.isArray(page.resources) || !page.resources.length ||
      page.resources.some((s) => { try { return new URL(s).origin !== origin || /ailerons/i.test(s); } catch { return true; } })) return false;
  if (kind === 'console') return url.pathname.startsWith('/ui/console/') && page.console === true && page.text.trim().length > 40;
  return url.pathname.startsWith('/ui/login/') && (kind === 'password' ? page.password === true : page.username === true);
}

const observationScript = `
  const root = document.querySelector('cnsl-root');
  const shown = (selector) => [...document.querySelectorAll(selector)].some(e => e.getClientRects().length);
  const text = document.body.innerText;
  const sample = document.querySelector('h1') || document.body;
  const font = getComputedStyle(sample).fontFamily;
  return {url:location.href, ready:document.readyState, title:document.title, text,
    secureContext:window.isSecureContext, webCrypto:typeof crypto.subtle?.digest === 'function',
    username:shown('#loginName'), password:shown('#password'),
    console:!!(root && root.querySelector('cnsl-header') && root.querySelector('cnsl-nav')),
    font, fontsReady:document.fonts.status === 'loaded',
    selectedFaceLoaded:[...document.fonts].some(f=>f.status==='loaded' &&
      f.family.replaceAll('"','').replaceAll("'",'')===font.split(',')[0].trim().replaceAll('"','').replaceAll("'",'')),
    failedFonts:[...document.fonts].filter(f=>f.status==='error').map(f=>f.family),
    resources:performance.getEntriesByType('resource').map(e=>e.name)};`;

async function run(output, expectedNamespace) {
  if (process.platform !== 'linux' || process.getuid() === 0 || !/^net:\[\d+\]$/.test(expectedNamespace) ||
      await readlink('/proc/self/ns/net') !== expectedNamespace) throw new Error('owned Linux network namespace required');
  const profile = await mkdtemp(join(tmpdir(), 'zitadel-browser-'));
  phase='driver';
  const driver = spawn('chromedriver', ['--port=9515', '--allowed-ips=127.0.0.1'], { stdio:'ignore', detached:true });
  let session, exited = false, spawnFailure = false, cleanup = false, sessionClosed = false;
  driver.once('error', () => { spawnFailure = true; });
  const exit = new Promise(resolve => driver.once('exit', () => { exited = true; resolve(); }));
  let id;
  const api = async (path, body, method = body === undefined ? 'GET' : 'POST') => {
    const response = await fetch('http://127.0.0.1:9515' + path, {
      method, redirect:'error', signal:AbortSignal.timeout(8000),
      headers:{'content-type':'application/json'}, ...(body === undefined ? {} : {body:JSON.stringify(body)}),
    });
    const text = await response.text();
    if (text.length > 4*1024*1024) throw new Error('oversized WebDriver response');
    const value = JSON.parse(text).value;
    if (!response.ok || value?.error) {
      category=['session not created','timeout','no such element','invalid argument','javascript error'].includes(value?.error) ? value.error.replaceAll(' ','-') : 'webdriver';
      throw new Error('WebDriver operation failed');
    }
    return value;
  };
  const call = (path, body, method) => api('/session/' + id + path, body, method);
  const evaluate = (script, args=[]) => call('/execute/sync', {script, args});
  const pause = () => new Promise(resolve => setTimeout(resolve, 200));
  const deadline = Date.now()+100000;
  const waitPage = async kind => {
    while (Date.now() < deadline) {
      if (spawnFailure || exited) throw new Error('owned browser driver exited');
      const page = await evaluate(observationScript);
      if (admitPage(page, kind)) return page;
      await pause();
    }
    throw new Error('native browser page did not become ready');
  };
  const capture = async name => {
    const png = await call('/screenshot');
    if (typeof png !== 'string' || png.length > 4*1024*1024) throw new Error('invalid screenshot');
    const bytes = Buffer.from(png, 'base64');
    if (!bytes.subarray(0,8).equals(Buffer.from([137,80,78,71,13,10,26,10]))) throw new Error('invalid screenshot bytes');
    await writeFile(join(output, name+'.png'), bytes, {flag:'wx', mode:0o600});
    return {path:name+'.png', bytes:bytes.length, sha256:createHash('sha256').update(bytes).digest('hex')};
  };
  let pages;
  try {
    const start = Date.now();
    for (;;) {
      if (spawnFailure || exited || Date.now()-start > 10000) throw new Error('browser driver unavailable');
      try { if ((await api('/status')).ready) break; } catch { /* bounded startup only */ }
      await pause();
    }
    phase='session';
    session = await api('/session', {capabilities:{alwaysMatch:{browserName:'chrome',
      'goog:chromeOptions':{args:['--headless=new', '--no-first-run', '--no-default-browser-check',
        '--disable-background-networking', '--window-size=1280,900', '--user-data-dir='+profile,
        '--host-resolver-rules=MAP fixture.localhost 127.0.0.1']}}}});
    id = session.sessionId;
    if (typeof id !== 'string' || !/^[a-f0-9]{32}$/.test(id) || typeof session.capabilities?.browserVersion !== 'string') throw new Error('invalid browser session');
    await call('/timeouts', {implicit:0, pageLoad:20000, script:10000});
    phase='login';
    await call('/url', {url:origin+'/ui/console/'});
    const login = await waitPage('login');
    const loginImage = await capture('login');
    const element = async selector => {
      const result = await call('/element', {using:'css selector', value:selector});
      const value = result?.['element-6066-11e4-a52e-4f735466cecf'];
      if (typeof value !== 'string') throw new Error('native form control absent');
      return '/element/'+encodeURIComponent(value);
    };
    await call(await element('#loginName')+'/value', {text:'operator@example.invalid'});
    await call(await element('#submit-button')+'/click', {});
    phase='password';
    const password = await waitPage('password');
    const passwordImage = await capture('password');
    await call(await element('#password')+'/value', {text:'FictionalOperatorOnly1!'});
    await call(await element('#submit-button')+'/click', {});
    phase='console';
    const consolePage = await waitPage('console');
    const consoleImage = await capture('console');
    pages = [login,password,consolePage].map((page,i) => ({kind:['login','password','console'][i],
      font:page.font, loaded:true, sameOriginResources:true, unadmittedFontAbsent:true, secureContext:page.secureContext, webCrypto:page.webCrypto,
      screenshot:[loginImage,passwordImage,consoleImage][i]}));
  } finally {
    if (id && !exited) { try { await call('', undefined, 'DELETE'); sessionClosed=true; } catch { /* unsuccessful closure is never success */ } }
    if (driver.pid && !exited) {
      try { process.kill(-driver.pid, 'SIGTERM'); } catch { /* verify exit below */ }
      await Promise.race([exit,new Promise(resolve=>setTimeout(resolve,2000))]);
      if (!exited) { try { process.kill(-driver.pid, 'SIGKILL'); } catch { /* verify exit below */ }
        await Promise.race([exit,new Promise(resolve=>setTimeout(resolve,2000))]); }
    }
    cleanup = exited && (!id || sessionClosed);
    if (cleanup) await rm(profile,{recursive:true});
  }
  if (!cleanup) throw new Error('browser cleanup incomplete');
  phase='receipt';
  const source = await readFile(fileURLToPath(import.meta.url));
  await writeFile(join(output,'browser-smoke.json'), JSON.stringify({schemaVersion:1, profile:'fictional-native-browser',
    browserSourceSha256:createHash('sha256').update(source).digest('hex'), browserVersion:session.capabilities.browserVersion,
    networkNamespace:expectedNamespace, browserSandboxDisabled:false, pages, cleanup:'complete',
    portalFederation:false, productionTLS:false, invitationProof:false, imageAdmission:'unknown', licenseClosure:'unknown'},null,2)+'\n', {flag:'wx',mode:0o600});
}

if (process.argv[1] === fileURLToPath(import.meta.url) && process.argv.length > 2) {
  if (process.argv.length !== 5 || process.argv[2] !== '--run') process.exitCode=1;
  else await run(process.argv[3],process.argv[4]).catch(() => { console.error('identity browser fixture failed stage='+phase+' category='+category); process.exitCode=1; });
}
