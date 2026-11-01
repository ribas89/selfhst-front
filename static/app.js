'use strict';

const grid = document.getElementById('grid');
const status = document.getElementById('status');
const search = document.getElementById('icon-search');
const previewColor = document.getElementById('preview-color');
const modalPreviewColor = document.getElementById('modal-preview-color');
const defaultPreviewColor = '#e5e5e5';
const sortOptions = document.getElementById('sortOptions');

// No persistence. Initial colors are gray/white/black by variant;
// a chosen custom color temporarily overrides all icon preview backgrounds.
function setPreviewColor(value) {
  if (!/^#[0-9a-fA-F]{6}$/.test(value)) return;
  previewColor.value = value;
  modalPreviewColor.value = value;
  document.documentElement.style.setProperty('--preview-bg', value);
  document.documentElement.classList.add('custom-preview-color');
}
previewColor.value = defaultPreviewColor;
modalPreviewColor.value = defaultPreviewColor;
previewColor.addEventListener('input', () => setPreviewColor(previewColor.value));
modalPreviewColor.addEventListener('input', () => setPreviewColor(modalPreviewColor.value));
const cards = [];
const dialog = document.getElementById('variants-dialog');
const dialogTitle = document.getElementById('dialog-title');
const variantsGrid = document.getElementById('variants-grid');
const closeDialog = document.getElementById('close-dialog');

const formatOrder = ['png', 'svg', 'webp', 'avif', 'ico'];
const styleOrder = ['default', 'dark', 'light'];

function sortedVariants(variants) {
  return variants.slice().sort((a, b) => {
    const aStyle = styleOrder.indexOf(a.style);
    const bStyle = styleOrder.indexOf(b.style);
    if (aStyle !== bStyle) return aStyle - bStyle;
    return formatOrder.indexOf(a.format) - formatOrder.indexOf(b.format);
  });
}

// The card uses the standard PNG whenever available. Other formats are only fallbacks.
function previewVariants(variants) {
  return sortedVariants(variants);
}

function addImage(container, urls, eager = false) {
  const img = document.createElement('img');
  img.alt = '';
  img.decoding = 'async';
  img.loading = eager ? 'eager' : 'lazy';
  let i = 0;
  img.addEventListener('error', () => {
    i += 1;
    if (i < urls.length) {
      img.src = urls[i];
    } else {
      img.remove();
    }
  });
  if (urls.length) img.src = urls[0];
  container.appendChild(img);
}

function showVariants(icon) {
  dialogTitle.textContent = icon.name;
  variantsGrid.replaceChildren();
  const fragment = document.createDocumentFragment();
  for (const variant of sortedVariants(icon.variants)) {
    const link = document.createElement('a');
    link.className = 'variant-card';
    link.dataset.style = variant.style;
    link.href = variant.url;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    link.setAttribute('aria-label', `${icon.name}: ${variant.format.toUpperCase()} ${variant.style === 'default' ? 'standard' : variant.style}. Open image in new tab`);

    const preview = document.createElement('span');
    preview.className = 'variant-preview';
    addImage(preview, [variant.url], true);

    const label = document.createElement('span');
    label.className = 'variant-label';
    const fmt = document.createElement('span');
    fmt.className = 'variant-format';
    fmt.textContent = variant.format;
    const style = document.createElement('span');
    style.className = 'variant-style';
    style.textContent = variant.style === 'default' ? 'Standard' : variant.style;
    label.append(fmt, style);
    link.append(preview, label);
    fragment.appendChild(link);
  }
  variantsGrid.appendChild(fragment);
  dialog.showModal();
}

function makeCard(icon) {
  const card = document.createElement('button');
  card.type = 'button';
  card.className = 'icon-card';
  card.setAttribute('aria-label', `Show versions of ${icon.name}`);

  const thumb = document.createElement('span');
  thumb.className = 'icon-thumb';
  addImage(thumb, previewVariants(icon.variants).map(variant => variant.url));

  const name = document.createElement('span');
  name.className = 'icon-name';
  name.textContent = icon.name;

  card.append(thumb, name);
  card.addEventListener('click', () => showVariants(icon));
  return card;
}

function applySearch() {
  const query = search.value.trim().toLocaleLowerCase();
  let shown = 0;
  for (const { element, terms } of cards) {
    const match = !query || terms.includes(query);
    element.hidden = !match;
    if (match) shown += 1;
  }
  status.textContent = shown === 0 ? 'No matching icons.' : '';
}

// Immediate filtering; no debounce.
search.addEventListener('input', applySearch);

function applySort() {
  const recent = sortOptions.value === 'recent';
  const ordered = cards.slice().sort((a, b) => {
    if (recent) {
      // Icons absent from the upstream index go after dated icons.
      if (a.added !== b.added) return b.added - a.added;
    }
    return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.slug.localeCompare(b.slug);
  });
  const fragment = document.createDocumentFragment();
  for (const card of ordered) fragment.appendChild(card.element);
  grid.appendChild(fragment);
}
sortOptions.addEventListener('change', applySort);

closeDialog.addEventListener('click', () => dialog.close());
dialog.addEventListener('click', event => {
  if (event.target === dialog) dialog.close();
});

async function start() {
  try {
    const response = await fetch('/api/catalog');
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const catalog = await response.json();
    if (!Array.isArray(catalog.items)) throw new Error('Invalid catalog');
    if (!catalog.items.length) {
      status.textContent = catalog.message || 'No icons found.';
      return;
    }
    // Keep all cards in the DOM; no pagination or extra catalog requests.
    const fragment = document.createDocumentFragment();
    for (const icon of catalog.items) {
      const element = makeCard(icon);
      const terms = [icon.name, icon.slug, icon.category || ''].join(' ').toLocaleLowerCase();
      const added = Date.parse(icon.createdAt || '') || 0;
      cards.push({ element, terms, added, name: icon.name, slug: icon.slug });
      fragment.appendChild(element);
    }
    grid.appendChild(fragment);
    applySort();
    applySearch();
  } catch (error) {
    status.textContent = 'Unable to load local icons.';
    console.error('Icon catalog:', error);
  }
}

start();
