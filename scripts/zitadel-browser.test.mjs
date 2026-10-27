import assert from 'node:assert/strict';
import test from 'node:test';
import { admitPage } from './zitadel-browser.mjs';

const login = {
  url: 'http://fixture.localhost:8080/ui/login/login?authRequest=fictional',
  ready: 'complete', title: 'Login', text: 'Sign in',
  password: false, username: true, console: false,
  font: 'Roboto, sans-serif', fontsReady: true, selectedFaceLoaded:true, secureContext:true, webCrypto:true,
  failedFonts: [], resources: ['http://fixture.localhost:8080/ui/login/resources/Roboto-Regular.woff2'],
};

test('admits the actual rendered login form and loaded fallback font', () => {
  assert.equal(admitPage(login, 'login'), true);
});

test('rejects incomplete, external, missing-font and wrong-page observations', () => {
  for (const change of [
    { url: 'https://external.invalid/ui/login/login' },
    { url: 'http://fixture.localhost:80/ui/login/login' },
    { url: 'http://user@fixture.localhost:8080/ui/login/login' },
    { url: 'http://fixture.localhost:8080/ui/console/' },
    { ready: 'loading' }, { text: '' }, { username: false },
    { font: 'Ailerons, sans-serif' }, { fontsReady: false }, { selectedFaceLoaded:false },
    { font:'' }, { fontsReady:'true' }, { resources:[] },
    { secureContext:false }, { webCrypto:false },
    { failedFonts: ['Roboto'] },
    { resources: ['https://external.invalid/font.woff2'] },
    { resources: ['http://fixture.localhost:8080/AILERONS.woff'] },
  ]) assert.equal(admitPage({ ...login, ...change }, 'login'), false, JSON.stringify(change));
  assert.equal(admitPage(null, 'login'), false);
  assert.equal(admitPage(login, 'unknown'), false);
});

test('the console requires its rendered shell, not the initial empty root element', () => {
  const page = { ...login, url: 'http://fixture.localhost:8080/ui/console/', username: false, console: true, text: 'Welcome to the management console. Users Projects Organizations' };
  assert.equal(admitPage(page, 'console'), true);
  assert.equal(admitPage({ ...page, console: false }, 'console'), false);
  assert.equal(admitPage({ ...page, text: '' }, 'console'), false);
});
