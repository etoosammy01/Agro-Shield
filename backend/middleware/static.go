package middleware

import (
	"net/http"
	"path"
	"strings"
)

// StaticCache sets explicit browser/CDN caching for the Go file server and
// prevents indexing of the duplicate page copies under /static/pages/.
func StaticCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := r.URL.Path
		switch {
		case strings.HasPrefix(requestPath, "/static/pages/"):
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		case strings.HasPrefix(requestPath, "/static/uploads/"):
			w.Header().Set("Cache-Control", "public, max-age=86400")
		default:
			switch strings.ToLower(path.Ext(requestPath)) {
			case ".css", ".js":
				// Revalidate code assets on every load. This app serves them
				// straight from disk with no content hash, so a one-hour cache
				// can leave a browser running an old form against a new server.
				w.Header().Set("Cache-Control", "no-cache")
			case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".ico", ".svg":
				w.Header().Set("Cache-Control", "public, max-age=86400")
			case ".woff", ".woff2", ".ttf", ".eot":
				w.Header().Set("Cache-Control", "public, max-age=2592000")
			default:
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
		}
		next.ServeHTTP(w, r)
	})
}

// NoDirectoryListing rejects directory URLs served by http.FileServer. The
// frontend tree has no index files, so without this Go would list uploaded
// files and every asset directory to anonymous visitors.
func NoDirectoryListing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
