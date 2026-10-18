// Optional browser regression suite. See docs/README.md for setup.
// Uses its own loopback server under a path prefix to exercise static hosting.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const http = require('node:http');
const { chromium } = require('playwright');
const siteDirectory = path.resolve(process.env.DOCS_SITE_DIR || __dirname);

async function main() {
 const server = http.createServer(async (request, response) => {
  try {
   const pathname = decodeURIComponent(new URL(request.url, 'http://localhost').pathname);
   if (!pathname.startsWith('/manual/')) throw new Error('Outside docs');
   const file = path.resolve(siteDirectory, pathname.slice('/manual/'.length) || 'index.html');
   if (!file.startsWith(siteDirectory + path.sep)) throw new Error('Outside docs');
   const content = await fs.readFile(file);
   const types = {'.html':'text/html', '.js':'text/javascript', '.css':'text/css', '.md':'text/markdown'};
   response.writeHead(200, {'Content-Type': (types[path.extname(file)] || 'application/octet-stream') + '; charset=utf-8'});
   response.end(content);
  } catch {
   response.writeHead(404); response.end('Not found');
  }
 });
 await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
 server.unref(); // A missing browser installation must not keep a failed run alive.
 const base = `http://127.0.0.1:${server.address().port}/manual/`;
 const browser = await chromium.launch({headless: true, executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined});
 const errors = [];
 const context = await browser.newContext({viewport: {width:1440, height:1000}, colorScheme:'light', permissions:['clipboard-read', 'clipboard-write']});
 const page = await context.newPage();
 page.setDefaultTimeout(6000);
 page.on('pageerror', error => errors.push(error.message));
 const waitPage = async slug => {
  await page.waitForURL(url => url.pathname === '/manual/' + slug + '.html');
  await page.waitForFunction(() => !document.querySelector('#content').hasAttribute('aria-busy'));
 };
 const noOverflow = async () => {
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1), `Page overflow: ${page.url()}`);
 };
 const screenshot = async name => {
  if (!process.env.DOCS_SCREENSHOT_DIR) return;
  await fs.mkdir(process.env.DOCS_SCREENSHOT_DIR, {recursive:true});
  await page.screenshot({path:path.join(process.env.DOCS_SCREENSHOT_DIR, name + '.png')});
 };
 try {
  await page.goto(base + 'overview.html');
  assert.equal(await page.locator('.nav-section[open]').count(), 1);
  assert.equal(await page.locator('#sidebar [aria-current="page"]').textContent(), 'Introduction');
  await noOverflow();
  await screenshot('desktop');
  await page.locator('#nav-expand').click();
  assert.equal(await page.locator('.nav-section[open]').count(), await page.locator('.nav-section').count());
  await page.locator('#nav-expand').click();
  assert.equal(await page.locator('.nav-section[open]').count(), 0);
  await page.evaluate(() => { window.shellMarker = true; });
  await page.locator('.nav-section[data-section="HTTP APIs"] summary').click();
  await page.locator('#sidebar a[href="responses.html"]').click();
  await waitPage('responses');
  assert(await page.evaluate(() => window.shellMarker), 'Internal navigation reloaded the shell');
  assert.equal(await page.locator('#sidebar [aria-current="page"]').count(), 1);
  assert.equal(await page.locator('[data-markdown]').first().getAttribute('href'), 'markdown/responses.md');
  assert((await page.title()).startsWith('Responses'));
  await page.locator('#toc a[href="#return-a-result"]').click();
  await page.waitForURL('**/responses.html#return-a-result');
  await page.waitForFunction(() => document.querySelector('#toc a[aria-current]').hash === '#return-a-result');
  assert((await page.locator('#return-a-result').boundingBox()).y >= 75, 'Anchor hidden behind header');
  await page.locator('article pre .copy').first().click();
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), await page.locator('article pre code').first().textContent());
  const position = await page.evaluate(() => scrollY);
  await page.locator('#sidebar a[href="requests.html"]').click();
  await waitPage('requests');
  await page.goBack();
  await page.waitForURL('**/responses.html#return-a-result');
  await page.waitForFunction(y => Math.abs(scrollY - y) < 3, position);
  await page.goForward();
  await waitPage('requests');

  await page.getByRole('button', {name:'Search documentation', exact:true}).click();
  assert.equal(await page.locator('#search-results a').count(), 6);
  await page.locator('#search-input').fill('response.JSON');
  assert.equal(await page.locator('#search-results a').first().getAttribute('href'), 'responses.html');
  assert(await page.locator('#search-results mark').count() > 0);
  await screenshot('search');
  await page.keyboard.press('ArrowDown');
  assert(await page.locator('#search-results a').first().evaluate(node => node === document.activeElement));
  await page.keyboard.press('Enter');
  await waitPage('responses');
  assert.equal(await page.locator('#search-dialog').evaluate(node => node.open), false);
  await page.keyboard.press('Control+k');
  await page.locator('#search-input').fill('zzzz-no-matching-guide');
  assert.equal(await page.locator('#search-results a').count(), 0);
  assert.match(await page.locator('#search-status').textContent(), /No matching sections/);
  for (const query of ['[', '(', '.*', 'x<script>']) await page.locator('#search-input').fill(query);
  await page.locator('#search-clear').click();
  assert.equal(await page.locator('#search-input').inputValue(), '');
  assert(await page.locator('#search-input').evaluate(node => node === document.activeElement));
  // Composition must not prematurely replace results or activate a link.
  await page.locator('#search-input').evaluate(node => {
   node.value = 'zzzz-no-matching-guide';
   node.dispatchEvent(new InputEvent('input', {bubbles:true, isComposing:true}));
   node.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', bubbles:true, isComposing:true}));
  });
  assert.equal(await page.locator('#search-results a').count(), 6);
  await page.locator('#search-input').dispatchEvent('compositionend');
  assert.equal(await page.locator('#search-results a').count(), 0);
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#search-dialog').evaluate(node => node.open), false);
  await page.locator('#theme').click();
  assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
  await page.reload();
  assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
  assert(await page.locator('.nav-section[data-section="HTTP APIs"]').evaluate(node => node.open));
  await screenshot('dark');

  await page.setViewportSize({width:390, height:844});
  await page.goto(base + 'overview.html');
  await page.locator('#theme').click();
  await screenshot('mobile');
  await page.locator('#menu').click();
  assert.equal(await page.locator('#navigation-dialog').evaluate(node => node.open), true);
  assert.equal(await page.locator('#sidebar').count(), 1, 'Mobile navigation duplicated');
  for (let i = 0; i < 25; i++) {
   await page.keyboard.press('Tab');
   assert(await page.evaluate(() => document.querySelector('#navigation-dialog').contains(document.activeElement)), 'Focus escaped the navigation dialog');
  }
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => !document.querySelector('#navigation-dialog').open);
  assert(await page.locator('#menu').evaluate(node => node === document.activeElement));
  await page.locator('#menu').click();
  await screenshot('drawer');
  await page.locator('.nav-section[data-section="Operations"] summary').click();
  await page.locator('#sidebar a[href="logging.html"]').click();
  await waitPage('logging');
  assert.equal(await page.locator('#navigation-dialog').evaluate(node => node.open), false);
  assert(await page.locator('#content').evaluate(node => node === document.activeElement));
  await noOverflow();
  await page.locator('.mobile-outline summary').click();
  const target = await page.locator('.mobile-outline a').nth(1).getAttribute('href');
  await page.locator('.mobile-outline a').nth(1).click();
  await page.waitForURL('**/logging.html' + target);
  assert.equal(await page.locator('.mobile-outline').evaluate(node => node.open), false);
  await page.getByRole('button', {name:'Search documentation', exact:true}).click();
  assert((await page.locator('#search-dialog').boundingBox()).width <= 390);
  await page.setViewportSize({width:390, height:360});
  const searchFooter = await page.locator('.search-footer').boundingBox();
  assert(searchFooter.y + searchFooter.height <= 360, 'Search controls clipped in a short viewport');
  await page.keyboard.press('Escape');

  // Long tables, code, diagrams and walkthroughs must stay inside the article.
  for (const width of [320, 768, 1280, 1440]) {
   await page.setViewportSize({width, height:900});
   for (const slug of ['configuration-reference', 'queues', 'onboarding', 'identity']) {
    await page.goto(base + slug + '.html');
    await noOverflow();
   }
  }
  await page.goto(base + 'application-lifecycle.html');
  await page.waitForSelector('.diagram[data-rendered] svg');
  await noOverflow();
  await screenshot('diagram');
  await page.emulateMedia({reducedMotion:'reduce', forcedColors:'active'});
  assert.equal(await page.locator('html').evaluate(node => getComputedStyle(node).scrollBehavior), 'auto');
  await noOverflow();
  await page.emulateMedia({forcedColors:'none'});

  // Failed enhanced navigation must fall back to the same working static URL.
  await page.goto(base + 'overview.html');
  await page.evaluate(() => { window.shellMarker = true; });
  await page.route('**/queues.html', route => route.request().resourceType() === 'fetch' ? route.abort() : route.continue());
  await page.locator('article a[href="queues.html"]').click();
  await waitPage('queues');
  assert.equal(await page.evaluate(() => window.shellMarker), undefined);
  await page.unroute('**/queues.html');

  const plain = await browser.newContext({javaScriptEnabled:false, viewport:{width:390, height:844}});
  const staticPage = await plain.newPage();
  await staticPage.goto(base + 'overview.html');
  assert(await staticPage.locator('#sidebar').isVisible());
  await staticPage.locator('.nav-section[data-section="Data and storage"] summary').click();
  await staticPage.locator('#sidebar a[href="migrations.html"]').click();
  await staticPage.waitForURL('**/migrations.html');
  assert(await staticPage.locator('article h1').isVisible());
  assert.equal(await staticPage.locator('[data-markdown]').first().getAttribute('href'), 'markdown/migrations.md');
  await plain.close();

  const missingIndex = await browser.newContext();
  const missingPage = await missingIndex.newPage();
  await missingPage.route('**/search-index.js*', route => route.abort());
  await missingPage.goto(base + 'overview.html');
  await missingPage.locator('#search-open').click();
  assert.match(await missingPage.locator('#search-status').textContent(), /Search could not load/);
  await missingIndex.close();

  const systemTheme = await browser.newContext({colorScheme:'dark'});
  const systemPage = await systemTheme.newPage();
  await systemPage.addInitScript(() => {
   Object.defineProperty(window, 'localStorage', {get() { throw new Error('Storage disabled'); }});
   Object.defineProperty(window, 'sessionStorage', {get() { throw new Error('Storage disabled'); }});
  });
  await systemPage.goto(base + 'overview.html');
  assert.equal(await systemPage.locator('html').getAttribute('data-theme'), 'dark');
  await systemPage.locator('#theme').click();
  assert.equal(await systemPage.locator('html').getAttribute('data-theme'), 'light');
  await systemTheme.close();
  assert.deepEqual(errors, [], 'Browser errors');
  console.log('Documentation browser checks passed: navigation, search, history, clipboard, themes, mobile, static hosting and fallbacks.');
 } finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
 }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
