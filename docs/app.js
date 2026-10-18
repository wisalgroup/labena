const theme = document.querySelector('#theme');
theme.addEventListener('click', () => {
 const dark = document.documentElement.dataset.theme !== 'dark';
 document.documentElement.dataset.theme = dark ? 'dark' : 'light';
 try { localStorage.setItem('docs-theme', dark ? 'dark' : 'light'); } catch {}
 window.docsVisuals?.refreshTheme();
});
const menu = document.querySelector('#menu');
const sidebar = document.querySelector('#sidebar');
function revealSelectedDoc() {
 const selected = sidebar.querySelector('[aria-current="page"]');
 if (!selected || !sidebar.clientHeight) return;
 const bounds = sidebar.getBoundingClientRect();
 const item = selected.getBoundingClientRect();
 const links = [...sidebar.querySelectorAll('a')];
 const last = links[Math.min(links.indexOf(selected) + 2, links.length - 1)];
 // Include the next two documents, even across groups, when they fit.
 const bottom = last.getBoundingClientRect().bottom;
 const offset = Math.min(bottom - bounds.top - sidebar.clientHeight, item.top - bounds.top);
 // Scroll only the navigation, leaving the article and URL anchor in place.
 if (item.top < bounds.top) sidebar.scrollTop += item.top - bounds.top;
 else if (offset > 0) sidebar.scrollTop += offset;
}
revealSelectedDoc();
window.addEventListener('resize', revealSelectedDoc);
menu.addEventListener('click', () => {
 const open = document.body.classList.toggle('menu-open');
 menu.setAttribute('aria-expanded', String(open));
 if (open) revealSelectedDoc();
});
document.querySelectorAll('#sidebar a').forEach(a => a.addEventListener('click', () => {
 document.body.classList.remove('menu-open'); menu.setAttribute('aria-expanded', 'false');
}));
const toc = document.querySelector('#toc');
let headings = [];
function updateToc() {
 let current = headings[0];
 if (!current) return;
 headings.forEach(h => { if (h.getBoundingClientRect().top < 170) current = h; });
 toc.querySelectorAll('a').forEach(a => {
 const active = a.hash === '#' + current.id;
 a.classList.toggle('active', active);
 if (active) a.setAttribute('aria-current', 'location'); else a.removeAttribute('aria-current');
 });
}
window.addEventListener('scroll', updateToc, {passive:true});
async function initializePage() {
 headings = [...document.querySelectorAll('article h2[id], article h3[id]')];
 toc.replaceChildren();
 const outline = document.createElement('ol');
 headings.forEach(h => {
  const label = h.cloneNode(true); label.querySelector('.heading-link')?.remove();
  const item = document.createElement('li'); item.className = 'toc-level-' + h.tagName.slice(1);
  const a = document.createElement('a'); a.href = '#' + encodeURIComponent(h.id); a.textContent = label.textContent;
  item.append(a); outline.append(item);
 });
 toc.append(outline);
 document.querySelectorAll('article pre').forEach(pre => {
  const code = pre.querySelector('code');
  if (!code) return;
  const language = [...code.classList].find(name => name.startsWith('language-'))?.slice(9);
  pre.dataset.language = ({sh:'Terminal',bash:'Terminal',shell:'Terminal',dotenv:'Environment',ini:'Environment',go:'Go',json:'JSON',yaml:'YAML',sql:'SQL',http:'HTTP',text:'Text',mermaid:'Diagram source'})[language] || 'Code';
  const button = document.createElement('button'); button.className = 'copy'; button.textContent = 'Copy'; button.setAttribute('aria-label', 'Copy code');
  button.addEventListener('click', async () => {
   try { await navigator.clipboard.writeText(code.textContent); button.textContent = 'Copied ✓'; setTimeout(() => button.textContent = 'Copy', 1800); }
   catch { const selection = window.getSelection(); const range = document.createRange(); range.selectNodeContents(code); selection.removeAllRanges(); selection.addRange(range); button.textContent = 'Selected — copy manually'; }
  });
  pre.append(button);
 });

 // Highlight supported languages only; unknown languages remain readable plain text.
 if (window.hljs) {
  document.querySelectorAll('article pre code:not([data-highlighted])').forEach(code => {
   const original = [...code.classList].find(name => name.startsWith('language-'));
   const language = original?.slice(9);
   const alias = {sh:'bash',shell:'bash',dotenv:'ini',text:'plaintext'}[language] || language;
   if (alias && hljs.getLanguage(alias)) {
    code.classList.replace(original, 'language-' + alias);
    hljs.highlightElement(code);
   }
  });
 }
 updateToc();
 await window.docsVisuals?.initialize(document.querySelector('article'));
}
const initialURL = new URL(location.href);
const initialScroll = history.state?.docsScroll;
const initialArticle = document.querySelector('article');
let initialLoading = true;
let pageReady = initializePage();
pageReady.then(() => {
 initialLoading = false;
 if (!initialArticle.isConnected || location.href !== initialURL.href) return;
 if (initialScroll || initialURL.hash) scrollToPage(initialURL, initialScroll, false);
});

function scrollToPage(url, position, focus = true) {
 let target;
 try { target = document.getElementById(decodeURIComponent(url.hash.slice(1))); } catch {}
 if (focus) document.querySelector('#content').focus({preventScroll: true});
 if (position) window.scrollTo({left: position[0], top: position[1], behavior: 'instant'});
 else if (target) target.scrollIntoView({behavior: 'instant'});
 else window.scrollTo({top: 0, left: 0, behavior: 'instant'});
 updateToc();
}
const searchDialog = document.querySelector('#search-dialog');
const searchInput = document.querySelector('#search-input');
const searchResults = document.querySelector('#search-results');
const searchStatus = document.querySelector('#search-status');
const searchOpen = document.querySelector('#search-open');
searchOpen.querySelector('kbd').textContent = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ K' : 'Ctrl K';
function openSearch() {
 window.docsVisuals?.close();
 if (!searchDialog.open) searchDialog.showModal();
 searchInput.focus();
}
searchOpen.addEventListener('click', openSearch);
document.querySelector('#search-close').addEventListener('click', () => searchDialog.close());
document.addEventListener('keydown', event => {
 if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
  event.preventDefault(); openSearch();
 }
});
// Keep identifiers intact while also allowing "request id" to find request_id
// and "require current user" to find RequireCurrentUser.
function searchable(value) {
 const expanded = value.replace(/([a-z\d])([A-Z])/g, '$1 $2').replace(/[_./:-]+/g, ' ');
 return value.toLowerCase() + ' ' + expanded.toLowerCase();
}
function rankSearch(entry, query, words) {
 const heading = searchable(entry.Heading || '');
 const title = searchable(entry.Title);
 const context = searchable([entry.Section, entry.Parent || ''].join(' '));
 const text = searchable(entry.Text);
 if (!words.every(word => (heading + ' ' + title + ' ' + context + ' ' + text).includes(word))) return 0;
 let score = words.reduce((total, word) => total + (heading.includes(word) ? 24 : 0) + (title.includes(word) ? 8 : 0) + (context.includes(word) ? 2 : 0), 1);
 if ((entry.Heading || '').toLowerCase() === query) score += 100;
 else if (heading.includes(query)) score += 35;
 if (entry.Title.toLowerCase() === query) score += entry.Heading ? 10 : 70;
 // Full API symbols in example code are more useful than loose word matches.
 if (text.includes(query) && /[._(]/.test(query)) score += 30;
 // Prefer the owning guide over an incidental example using the same API.
 // "response.JSON" belongs with Responses; "cache.Remember" belongs with Cache.
 const packageName = query.match(/^([a-z][a-z0-9]*)\./)?.[1];
 if (packageName && title.includes(packageName)) score += entry.Heading ? 40 : 50;
 return score;
}
function searchExcerpt(entry, words) {
 const lower = entry.Text.toLowerCase();
 const positions = words.map(word => lower.indexOf(word)).filter(position => position >= 0);
 const start = Math.max(0, (positions.length ? Math.min(...positions) : 0) - 55);
 const end = Math.min(entry.Text.length, start + 220);
 return (start ? '…' : '') + entry.Text.slice(start, end) + (end < entry.Text.length ? '…' : '');
}
searchInput.addEventListener('input', () => {
 const query = searchInput.value.trim().toLowerCase();
 searchResults.replaceChildren();
 if (!query) { searchStatus.textContent = 'Search guides, sections, and API names.'; return; }
 const words = query.split(/\s+/);
 const matches = (window.docsSearch || []).map(page => ({page, score: rankSearch(page, query, words)}))
  .filter(result => result.score > 0).sort((a,b) => b.score - a.score);
 const results = matches.slice(0, 20);
 searchStatus.textContent = results.length ? `${matches.length > 20 ? 'Showing the first 20 of ' : ''}${matches.length} matching ${matches.length === 1 ? 'result' : 'results'}. Tab to a result and press Enter.` : 'No matching sections. Try a shorter term or an API name.';
 for (const {page} of results) {
  const item = document.createElement('li');
  const link = document.createElement('a'); link.href = page.URL;
  const context = document.createElement('span'); context.className = 'search-context'; context.textContent = [page.Section, page.Title, page.Parent].filter(Boolean).join(' / ');
  const title = document.createElement('strong'); title.textContent = page.Heading || page.Title;
  const excerpt = document.createElement('small');
  excerpt.textContent = searchExcerpt(page, words);
  link.append(context, title, excerpt); item.append(link); searchResults.append(item);
 }
});

// Keep the shell mounted while loading generated pages. Ordinary links still
// work on static hosts, with JavaScript disabled, or when opened from disk.
if (location.protocol === 'http:' || location.protocol === 'https:') {
 const content = document.querySelector('#content');
 const docsRoot = new URL('.', document.querySelector('.brand').href);
 const pagePath = url => url.pathname === docsRoot.pathname ? docsRoot.pathname + 'index.html' : url.pathname;
 const pageKey = url => pagePath(url) + url.search;
 const pageLinks = [...sidebar.querySelectorAll('a[href]')];
 const pagePaths = new Set(pageLinks.map(link => new URL(link.href).pathname));
 let renderedPage = pageKey(new URL(location.href));
 let navigation;
 let scrollTimer;

 function saveScroll() {
  if (!navigation && !initialLoading) history.replaceState({...history.state, docsScroll: [scrollX, scrollY]}, '');
 }
 history.scrollRestoration = 'manual';
 window.addEventListener('scroll', () => {
  clearTimeout(scrollTimer);
  scrollTimer = setTimeout(saveScroll, 100);
 }, {passive: true});
 window.addEventListener('pagehide', saveScroll);
 window.addEventListener('pageshow', () => {
  const position = history.state?.docsScroll;
  if (position && !initialLoading) window.scrollTo({left: position[0], top: position[1], behavior: 'instant'});
 });

 async function navigate(url, {restore = false, position} = {}) {
  navigation?.abort();
  window.docsVisuals?.close();
  clearTimeout(scrollTimer);
  const request = new AbortController();
  navigation = request;
  content.setAttribute('aria-busy', 'true');
  try {
   if (pageKey(url) !== renderedPage) {
    const response = await fetch(url.pathname + url.search, {signal: request.signal});
    const destination = new URL(response.url);
    if (!response.ok || destination.origin !== url.origin || pageKey(destination) !== pageKey(url)) throw new Error('Page unavailable');
    const next = new DOMParser().parseFromString(await response.text(), 'text/html');
    const nextContent = next.querySelector('#content');
    const description = next.querySelector('meta[name="description"]');
    const download = next.querySelector('.source-link[download]');
    if (!nextContent || !description || !download) throw new Error('Invalid documentation page');
    if (request.signal.aborted) return;

    content.replaceChildren(...nextContent.childNodes);
    document.title = next.title;
    document.querySelector('meta[name="description"]').content = description.content;
    document.querySelectorAll('.header-actions a[download], .source-link[download]').forEach(link => {
     link.setAttribute('href', download.getAttribute('href'));
    });
    pageLinks.forEach(link => {
     const selected = new URL(link.href).pathname === pagePath(url);
     link.classList.toggle('selected', selected);
     if (selected) link.setAttribute('aria-current', 'page'); else link.removeAttribute('aria-current');
    });
    renderedPage = pageKey(url);
    pageReady = initializePage();
   }
   if (!restore && url.href !== location.href) history.pushState({}, '', url);
   if (searchDialog.open) searchDialog.close();
   document.body.classList.remove('menu-open');
   menu.setAttribute('aria-expanded', 'false');
   revealSelectedDoc();
   await pageReady;
   if (request.signal.aborted) return;
   scrollToPage(url, position);
  } catch (error) {
   if (request.signal.aborted) return;
   // A failed fetch must not turn a working static link into a dead end.
   if (restore) location.replace(url.href); else location.assign(url.href);
  } finally {
   if (navigation === request) {
    navigation = null;
    content.removeAttribute('aria-busy');
    saveScroll();
   }
  }
 }

 document.addEventListener('click', event => {
  if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  const link = event.target.closest('a[href]');
  if (!link || link.hasAttribute('download') || (link.target && link.target !== '_self')) return;
  const url = new URL(link.href);
  if (url.origin !== location.origin || !pagePaths.has(pagePath(url))) return;
  event.preventDefault();
  saveScroll();
  navigate(url);
 });
 window.addEventListener('popstate', event => {
  navigate(new URL(location.href), {restore: true, position: event.state?.docsScroll});
 });
}
