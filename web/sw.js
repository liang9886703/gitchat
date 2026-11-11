'use strict';

// Public assets only. Never store API responses, cookies, conversations or share URLs.
const CACHE = 'gitchat-shell-v4';
const ASSETS = ['/offline.html', '/style.css', '/platform.css', '/models.css', '/models.js', '/app.js', '/platform.js', '/manifest.webmanifest', '/icons/icon.svg', '/icons/icon-192.png', '/icons/icon-512.png', '/icons/maskable-512.png', '/icons/apple-touch-icon.png'];
self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(ASSETS)));
});
self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    for (const key of await caches.keys()) {
      if (key.startsWith('gitchat-shell-') && key !== CACHE) await caches.delete(key);
    }
    await self.clients.claim();
  })());
});
self.addEventListener('message', event => {
  if (event.data?.type === 'ACTIVATE_UPDATE') self.skipWaiting();
});
self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET' || url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/g/') || url.pathname === '/health') return;
  if (event.request.mode === 'navigate' && url.pathname === '/') {
    // Do not cache the page response or a URL containing a private share token.
    if (self.navigator.onLine === false) { event.respondWith(caches.match('/offline.html')); return; }
    event.respondWith(fetch(event.request).catch(() => caches.match('/offline.html')));
    return;
  }
  if (ASSETS.includes(url.pathname) && !url.search) {
    if (self.navigator.onLine === false) { event.respondWith(caches.match(url.pathname)); return; }
    // Network first keeps HTML and JS versions in sync; the installed cache is a fallback.
    event.respondWith(fetch(event.request).then(response => response.ok ? response : caches.match(url.pathname)).catch(() => caches.match(url.pathname)));
  }
});
