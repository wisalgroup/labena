const theme = document.querySelector('#theme');
function updateThemeLabel() {
 const label = `Switch to ${document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark'} theme`;
 theme.setAttribute('aria-label', label);
 theme.title = label;
}
updateThemeLabel();
theme.addEventListener('click', () => {
 const value = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
 document.documentElement.dataset.theme = value;
 try { localStorage.setItem('docs-theme', value); } catch {}
 updateThemeLabel();
 window.docsVisuals?.refreshTheme();
});

// One navigation tree serves the desktop rail and the native mobile dialog.
const menu = document.querySelector('#menu');
const sidebar = document.querySelector('#sidebar');
const sidebarSlot = document.querySelector('#sidebar-slot');
const navigationDialog = document.querySelector('#navigation-dialog');
const narrowScreen = matchMedia('(max-width: 820px)');
const groups = [...sidebar.querySelectorAll('.nav-section')];
const expandAll = document.querySelector('#nav-expand');
let savedGroups = {};
try { savedGroups = JSON.parse(sessionStorage.getItem('docs-groups')) || {}; } catch {}
function updateExpandLabel() {
 expandAll.textContent = groups.every(group => group.open) ? 'Collapse all' : 'Expand all';
}
for (const group of groups) {
 if (typeof savedGroups[group.dataset.section] === 'boolean') group.open = savedGroups[group.dataset.section];
 if (group.querySelector('[aria-current="page"]')) group.open = true;
 group.addEventListener('toggle', () => {
  savedGroups[group.dataset.section] = group.open;
  try { sessionStorage.setItem('docs-groups', JSON.stringify(savedGroups)); } catch {}
  updateExpandLabel();
 });
}
updateExpandLabel();
expandAll.addEventListener('click', () => {
 const open = !groups.every(group => group.open);
 groups.forEach(group => { group.open = open; });
 updateExpandLabel();
});
function revealSelectedDoc() {
 const selected = sidebar.querySelector('[aria-current="page"]');
 if (!selected) return;
 selected.closest('details').open = true;
 if (!sidebar.clientHeight) return;
 const bounds = sidebar.getBoundingClientRect();
 const item = selected.getBoundingClientRect();
 if (item.top < bounds.top + 12) sidebar.scrollTop += item.top - bounds.top - 12;
 else if (item.bottom > bounds.bottom - 24) sidebar.scrollTop += item.bottom - bounds.bottom + 24;
}
function restoreNavigation() {
 if (sidebar.parentElement !== sidebarSlot) sidebarSlot.append(sidebar);
 menu.setAttribute('aria-expanded', 'false');
}
function closeNavigation() {
 if (navigationDialog.open) navigationDialog.close();
 restoreNavigation();
}
menu.addEventListener('click', () => {
 if (navigationDialog.open) { closeNavigation(); return; }
 navigationDialog.append(sidebar);
 navigationDialog.showModal();
 menu.setAttribute('aria-expanded', 'true');
 revealSelectedDoc();
});
document.querySelector('#navigation-close').addEventListener('click', closeNavigation);
navigationDialog.addEventListener('close', restoreNavigation);
sidebar.addEventListener('click', event => {
 if (event.target.closest('a[href]')) closeNavigation();
});
window.addEventListener('resize', () => {
 if (!narrowScreen.matches) closeNavigation();
 revealSelectedDoc();
});
revealSelectedDoc();

const toc = document.querySelector('#toc');
let headings = [];
let activeHeading;
let tocFrame;
function updateToc() {
 let current = headings[0];
 if (!current) return;
 const offset = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--header-height')) + 48;
 headings.forEach(h => { if (h.getBoundingClientRect().top < offset) current = h; });
 if (current === activeHeading) return;
 activeHeading = current;
 for (const a of document.querySelectorAll('#toc a, .mobile-outline a')) {
  const active = decodeURIComponent(a.hash.slice(1)) === current.id;
  a.classList.toggle('active', active);
  if (active) a.setAttribute('aria-current', 'location'); else a.removeAttribute('aria-current');
 }
 const selected = toc.querySelector('[aria-current]');
 if (selected && toc.clientHeight) {
  const bounds = toc.getBoundingClientRect();
  const item = selected.getBoundingClientRect();
  if (item.top < bounds.top) toc.scrollTop += item.top - bounds.top;
  else if (item.bottom > bounds.bottom) toc.scrollTop += item.bottom - bounds.bottom;
 }
}
window.addEventListener('scroll', () => {
 if (tocFrame) return;
 tocFrame = requestAnimationFrame(() => { tocFrame = null; updateToc(); });
}, {passive: true});
async function initializePage() {
 headings = [...document.querySelectorAll('article h2[id], article h3[id]')];
 activeHeading = null;
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
   try {
    await navigator.clipboard.writeText(code.textContent);
    button.textContent = 'Copied ✓'; button.setAttribute('aria-label', 'Code copied');
    setTimeout(() => { button.textContent = 'Copy'; button.setAttribute('aria-label', 'Copy code'); }, 1800);
   } catch {
    const selection = window.getSelection(); const range = document.createRange(); range.selectNodeContents(code);
    selection.removeAllRanges(); selection.addRange(range);
    button.textContent = 'Selected — copy manually'; button.setAttribute('aria-label', 'Code selected; copy manually');
   }
  });
  pre.append(button);
 });
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
const searchClear = document.querySelector('#search-clear');
searchOpen.querySelector('kbd').textContent = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ K' : 'Ctrl K';
function openSearch() {
 closeNavigation();
 window.docsVisuals?.close();
 if (!searchDialog.open) searchDialog.showModal();
 searchInput.focus();
 renderSearch();
}
searchOpen.addEventListener('click', openSearch);
document.querySelector('#search-close').addEventListener('click', () => searchDialog.close());
for (const dialog of [navigationDialog, searchDialog]) {
 // Dismiss only a click outside the dialog, never whitespace inside it.
 dialog.addEventListener('click', event => {
  if (event.target !== dialog) return;
  const bounds = dialog.getBoundingClientRect();
  if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) dialog.close();
 });
}
document.addEventListener('keydown', event => {
 if (event.isComposing) return;
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
function highlightMatch(element, text, words) {
 // Build text nodes, not HTML, because the search query is reader input.
 const pattern = words.filter(Boolean).map(word => word.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|');
 if (!pattern) { element.textContent = text; return; }
 const matches = text.matchAll(new RegExp(pattern, 'gi'));
 let cursor = 0;
 for (const match of matches) {
  element.append(document.createTextNode(text.slice(cursor, match.index)));
  const mark = document.createElement('mark'); mark.textContent = match[0]; element.append(mark);
  cursor = match.index + match[0].length;
 }
 element.append(document.createTextNode(text.slice(cursor)));
}
function renderSearch() {
 const query = searchInput.value.trim().toLowerCase();
 const words = query.split(/\s+/).filter(Boolean);
 searchClear.hidden = !searchInput.value;
 searchResults.replaceChildren();
 if (!window.docsSearch) {
  searchStatus.textContent = 'Search could not load. Reload the page, or browse the topic groups.';
  return;
 }
 const suggestions = ['overview.html', 'index.html', 'modules.html', 'routing.html', 'queues.html', 'deployment.html'];
 const matches = query
  ? window.docsSearch.map(page => ({page, score: rankSearch(page, query, words)})).filter(result => result.score > 0).sort((a,b) => b.score - a.score)
  : suggestions.map(url => ({page: window.docsSearch.find(page => page.URL === url)})).filter(result => result.page);
 searchStatus.textContent = !query ? 'Start with a guide, or search for a topic or API symbol.'
  : !matches.length ? 'No matching sections. Try a shorter term, such as “queues” or “response.JSON”.'
  : `${matches.length > 20 ? 'Showing 20 of ' : ''}${matches.length} matching ${matches.length === 1 ? 'result' : 'results'}.`;
 for (const {page} of matches.slice(0, 20)) {
  const item = document.createElement('li');
  const link = document.createElement('a'); link.href = page.URL;
  const context = document.createElement('span'); context.className = 'search-context';
  context.textContent = [page.Section, ...(page.Heading ? [page.Title, page.Parent] : [])].filter(Boolean).join(' / ');
  const title = document.createElement('strong'); highlightMatch(title, page.Heading || page.Title, words);
  const excerpt = document.createElement('small'); highlightMatch(excerpt, searchExcerpt(page, words), words);
  link.append(context, title, excerpt); item.append(link); searchResults.append(item);
 }
 searchResults.scrollTop = 0;
}
searchInput.addEventListener('input', event => { if (!event.isComposing) renderSearch(); });
searchInput.addEventListener('compositionend', renderSearch);
searchClear.addEventListener('click', () => { searchInput.value = ''; renderSearch(); searchInput.focus(); });
searchDialog.addEventListener('keydown', event => {
 if (event.isComposing || event.metaKey || event.ctrlKey || event.altKey) return;
 if (event.key === 'Escape') {
  // Search inputs may otherwise consume Escape to clear their value first.
  event.preventDefault(); searchDialog.close(); return;
 }
 const links = [...searchResults.querySelectorAll('a')];
 const index = links.indexOf(document.activeElement);
 if (event.key === 'ArrowDown' && (document.activeElement === searchInput || index >= 0)) {
  event.preventDefault(); links[Math.min(index + 1, links.length - 1)]?.focus();
 } else if (event.key === 'ArrowUp' && index >= 0) {
  event.preventDefault(); (index === 0 ? searchInput : links[index - 1]).focus();
 } else if (event.key === 'Enter' && document.activeElement === searchInput) {
  event.preventDefault(); links[0]?.click();
 }
});
// Keep the shell mounted while loading generated pages. Ordinary links still
// work on static hosts, with JavaScript disabled, or when opened from disk.
if (location.protocol === 'http:' || location.protocol === 'https:') {
 const content = document.querySelector('#content');
 const docsRoot = new URL('.', document.querySelector('.brand').href);
 const pagePath = url => url.pathname === docsRoot.pathname ? docsRoot.pathname + 'index.html' : url.pathname;
 const pageKey = url => pagePath(url) + url.search;
 const pageLinks = [...sidebar.querySelectorAll('.nav-pages a[href]')];
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
    document.querySelectorAll('[data-markdown]').forEach(link => {
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
   closeNavigation();
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
  const mobileOutline = link.closest('.mobile-outline');
  if (mobileOutline) mobileOutline.open = false;
  navigate(url);
 });
 window.addEventListener('popstate', event => {
  navigate(new URL(location.href), {restore: true, position: event.state?.docsScroll});
 });
}
