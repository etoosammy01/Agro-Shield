const CACHE_NAME = 'agro-shield-v1';

const STATIC_ASSETS = [
    '/',
    '/offline',
    '/static/css/style.css',
    '/static/css/dashboard.css',
    '/static/css/marketplace.css',
    '/static/js/navigation.js',
    '/static/assets/logo/Agro-Shield_logo.png',
    '/static/assets/favicon_io/favicon-32x32.png',
    '/static/assets/favicon_io/apple-touch-icon.png',
    '/static/assets/favicon_io/site.webmanifest'
];

// Install Event - Pre-cache essential offline assets
self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE_NAME).then((cache) => {
            return cache.addAll(STATIC_ASSETS).catch((err) => {
                console.warn('PWA service worker asset caching error:', err);
            });
        }).then(() => self.skipWaiting())
    );
});

// Activate Event - Clean up old caches
self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys().then((cacheNames) => {
            return Promise.all(
                cacheNames.map((cache) => {
                    if (cache !== CACHE_NAME) {
                        return caches.delete(cache);
                    }
                })
            );
        }).then(() => self.clients.claim())
    );
});

// Fetch Event - Network first for pages & dynamic requests, fallback to offline cache/page
self.addEventListener('fetch', (event) => {
    const request = event.request;

    // Only handle GET requests
    if (request.method !== 'GET') return;

    // Skip non-http/https requests
    if (!request.url.startsWith('http')) return;

    const url = new URL(request.url);

    // Static assets (CSS, JS, images, icons): Cache first, fallback to network
    if (url.pathname.startsWith('/static/')) {
        event.respondWith(
            caches.match(request).then((cachedResponse) => {
                if (cachedResponse) {
                    // Update cache in background
                    fetch(request).then((networkResponse) => {
                        if (networkResponse && networkResponse.status === 200) {
                            caches.open(CACHE_NAME).then((cache) => cache.put(request, networkResponse));
                        }
                    }).catch(() => {});
                    return cachedResponse;
                }
                return fetch(request).then((networkResponse) => {
                    if (networkResponse && networkResponse.status === 200) {
                        const responseToCache = networkResponse.clone();
                        caches.open(CACHE_NAME).then((cache) => cache.put(request, responseToCache));
                    }
                    return networkResponse;
                });
            })
        );
        return;
    }

    // Navigation / Page requests: Network first, fallback to cached page or offline.html
    if (request.mode === 'navigate' || request.headers.get('accept')?.includes('text/html')) {
        event.respondWith(
            fetch(request)
                .then((networkResponse) => {
                    if (networkResponse && networkResponse.status === 200) {
                        const responseToCache = networkResponse.clone();
                        caches.open(CACHE_NAME).then((cache) => cache.put(request, responseToCache));
                    }
                    return networkResponse;
                })
                .catch(() => {
                    return caches.match(request).then((cachedResponse) => {
                        if (cachedResponse) return cachedResponse;
                        return caches.match('/offline');
                    });
                })
        );
        return;
    }

    // Default network fetch with cache fallback
    event.respondWith(
        fetch(request).catch(() => caches.match(request))
    );
});