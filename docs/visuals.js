// Shared enhancements for diagrams and Markdown walkthroughs. Both retain their
// original explanation when scripting or rendering is unavailable.
window.docsVisuals = (() => {
 const rendererURL = new URL('vendor/mermaid.min.js', document.currentScript.src);
 let rendererPromise;
 let renderQueue = Promise.resolve();
 let nextID = 0;
 let expanded;

 function loadRenderer() {
  if (!rendererPromise) rendererPromise = new Promise((resolve, reject) => {
   const script = document.createElement('script');
   script.src = rendererURL.href;
   const timer = setTimeout(() => fail(), 10000);
   function fail() {
    clearTimeout(timer);
    script.remove();
    rendererPromise = null;
    reject(new Error('Diagram renderer unavailable'));
   }
   script.onload = () => {
    clearTimeout(timer);
    if (window.mermaid) resolve(window.mermaid); else fail();
   };
   script.onerror = fail;
   document.head.append(script);
  });
  return rendererPromise;
 }

 function button(label, action) {
  const control = document.createElement('button');
  control.type = 'button';
  control.textContent = label;
  control.addEventListener('click', action);
  return control;
 }

 function closeExpanded() { expanded?.(); }

 function scrollHint() {
  const hint = document.createElement('p');
  hint.className = 'diagram-hint';
  hint.textContent = 'Scroll to explore the full diagram.';
  return hint;
 }

 function expandDiagram(figure, viewport, trigger) {
  closeExpanded();
  const dialog = document.createElement('dialog');
  dialog.className = 'diagram-dialog';
  const header = document.createElement('div');
  header.className = 'visual-heading';
  const title = document.createElement('h2');
  title.id = 'expanded-diagram-title';
  title.textContent = figure.querySelector('figcaption').textContent;
  dialog.setAttribute('aria-labelledby', title.id);
  header.append(title, button('Close', close));
  // Move the SVG instead of cloning it: marker and accessibility IDs stay unique.
  const placeholder = document.createElement('div');
  placeholder.style.height = viewport.getBoundingClientRect().height + 'px';
  viewport.replaceWith(placeholder);
  dialog.append(header, scrollHint(), viewport);
  function close() {
   if (!dialog.isConnected) return;
   dialog.close();
   placeholder.replaceWith(viewport);
   dialog.remove();
   expanded = null;
   if (trigger.isConnected) trigger.focus({preventScroll: true});
  }
  dialog.addEventListener('close', close, {once: true});
  document.body.append(dialog);
  expanded = close;
  dialog.showModal();
 }

 function renderDiagram(figure) {
  // Mermaid's configuration is global. Serialize rendering so a theme change or
  // a fast page switch cannot mix configurations or update a detached article.
  const revision = figure.revision = (figure.revision || 0) + 1;
  const task = renderQueue.then(async () => {
   if (!figure.isConnected || revision !== figure.revision) return;
   const viewport = figure.querySelector('.diagram-viewport');
   const status = figure.querySelector('.diagram-status');
   const source = figure.querySelector('code.language-mermaid').textContent;
   try {
    const renderer = await loadRenderer();
    if (!figure.isConnected || revision !== figure.revision) return;
    const styles = getComputedStyle(document.documentElement);
    const color = name => styles.getPropertyValue(name).trim();
    renderer.initialize({
     startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true,
     theme: 'base', fontFamily: getComputedStyle(document.body).fontFamily,
     flowchart: {htmlLabels: false, useMaxWidth: true, curve: 'linear'},
     sequence: {useMaxWidth: true},
     themeVariables: {
      darkMode: document.documentElement.dataset.theme === 'dark',
      background: color('--bg'), primaryColor: color('--wash'),
      primaryTextColor: color('--text'), primaryBorderColor: color('--accent'),
      secondaryColor: color('--inline'), tertiaryColor: color('--bg'),
      lineColor: color('--muted'), textColor: color('--text'),
      mainBkg: color('--wash'), nodeBorder: color('--accent'),
      clusterBkg: color('--bg'), clusterBorder: color('--line'),
      edgeLabelBackground: color('--bg'), actorBkg: color('--wash'),
      actorBorder: color('--accent'), actorTextColor: color('--text'),
      actorLineColor: color('--muted'), signalColor: color('--text'),
      signalTextColor: color('--text'), labelBoxBkgColor: color('--wash'),
      labelTextColor: color('--text'), loopTextColor: color('--text'),
      noteBkgColor: color('--inline'), noteTextColor: color('--text'),
      noteBorderColor: color('--line'), fontSize: '15px'
     }
    });
    const {svg} = await renderer.render('docs-diagram-' + ++nextID, source);
    if (!figure.isConnected || revision !== figure.revision) return;
    viewport.innerHTML = svg;
    const drawing = viewport.querySelector('svg');
    drawing.style.minWidth = Math.min(drawing.viewBox.baseVal.width, 640) + 'px';
    status.hidden = true;
    figure.querySelector('button').disabled = false;
    figure.dataset.rendered = 'true';
   } catch {
    if (!figure.isConnected || revision !== figure.revision) return;
    status.hidden = false;
    status.textContent = 'The diagram could not be rendered. Read the explanation or view its source below.';
    figure.querySelector('details').open = true;
   }
  });
  renderQueue = task.catch(() => {});
  return task;
 }

 function createDiagram(code) {
  const pre = code.closest('pre');
  const source = code.textContent;
  const figure = document.createElement('figure');
  figure.className = 'diagram';
  const header = document.createElement('div');
  header.className = 'visual-heading';
  const caption = document.createElement('figcaption');
  caption.textContent = source.match(/^\s*accTitle:\s*(.+)$/m)?.[1] || 'Diagram';
  const viewport = document.createElement('div');
  viewport.className = 'diagram-viewport';
  viewport.tabIndex = 0;
  viewport.setAttribute('role', 'region');
  viewport.setAttribute('aria-label', caption.textContent + ' — scroll to explore');
  const expand = button('Expand', () => expandDiagram(figure, viewport, expand));
  expand.setAttribute('aria-label', 'Expand diagram: ' + caption.textContent);
  expand.disabled = true;
  header.append(caption, expand);
  figure.append(header);
  const description = source.match(/^\s*accDescr:\s*(.+)$/m)?.[1] || source.match(/^\s*accDescr\s*\{([\s\S]*?)\}/m)?.[1];
  if (description) {
   const explanation = document.createElement('p');
   explanation.className = 'diagram-description';
   explanation.textContent = description.trim();
   figure.append(explanation);
  }
  const status = document.createElement('p');
  status.className = 'diagram-status';
  status.setAttribute('role', 'status');
  status.textContent = 'Rendering diagram…';
  const details = document.createElement('details');
  const summary = document.createElement('summary');
  summary.textContent = 'Diagram source';
  details.append(summary);
  pre.replaceWith(figure);
  details.append(pre);
  figure.append(status, scrollHint(), viewport, details);
  return figure;
 }

 function revealStep(nav) {
  const selected = nav.querySelector('[aria-current="step"]');
  if (!selected) return;
  const bounds = nav.getBoundingClientRect();
  const item = selected.getBoundingClientRect();
  if (item.left < bounds.left) nav.scrollLeft += item.left - bounds.left;
  else if (item.right > bounds.right) nav.scrollLeft += item.right - bounds.right;
 }
 window.addEventListener('resize', () => {
  document.querySelectorAll('.walkthrough-nav').forEach(revealStep);
 });

 function initializeWalkthrough(section) {
  const list = section.querySelector(':scope > ol');
  if (!list || list.children.length < 2 || section.dataset.enhanced) return;
  const steps = [...list.children];
  const nav = document.createElement('nav');
  nav.className = 'walkthrough-nav';
  nav.setAttribute('aria-label', section.querySelector('h3').textContent + ' steps');
  const footer = document.createElement('div');
  footer.className = 'walkthrough-controls';
  const status = document.createElement('span');
  status.setAttribute('role', 'status');
  let current = 0;
  const labels = steps.map((step, index) => step.querySelector('strong')?.textContent.replace(/[.:]$/, '') || 'Step ' + (index + 1));
  const controls = steps.map((step, index) => {
   step.id = 'docs-step-' + ++nextID;
   const control = button(labels[index], () => select(index));
   const number = document.createElement('span');
   number.textContent = index + 1;
   number.setAttribute('aria-hidden', 'true');
   control.prepend(number);
   control.setAttribute('aria-controls', step.id);
   nav.append(control);
   return control;
  });
  const previous = button('Previous', () => select(current - 1));
  const next = button('Next', () => select(current + 1));
  function select(index) {
   current = Math.max(0, Math.min(index, steps.length - 1));
   steps.forEach((step, i) => {
    step.classList.toggle('current', i === current);
    step.inert = i !== current;
    step.setAttribute('aria-hidden', String(i !== current));
    if (i === current) controls[i].setAttribute('aria-current', 'step'); else controls[i].removeAttribute('aria-current');
   });
   status.textContent = `${current + 1} / ${steps.length} · ${labels[current]}`;
   previous.disabled = current === 0;
   next.disabled = current === steps.length - 1;
   revealStep(nav);
  }
  footer.append(previous, status, next);
  list.classList.add('walkthrough-body');
  list.before(nav);
  list.after(footer);
  section.dataset.enhanced = 'true';
  select(0);
 }

 return {
  async initialize(article) {
   article.querySelectorAll('.walkthrough').forEach(initializeWalkthrough);
   const figures = [...article.querySelectorAll('pre > code.language-mermaid')]
    .filter(code => !code.closest('.diagram')).map(createDiagram);
   await Promise.all(figures.map(renderDiagram));
  },
  refreshTheme() {
   closeExpanded();
   return Promise.all([...document.querySelectorAll('article .diagram')].map(renderDiagram));
  },
  close: closeExpanded
 };
})();
