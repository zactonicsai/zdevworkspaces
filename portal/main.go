// devspaces portal — a small Go service that serves the developer web UI and a
// JSON API for creating and managing workspaces. Standard library only.
//
//	portal -mock -dev-user alice@example.com          # demo: in-memory cluster, no Kubernetes needed
//	portal -dev-user alice@example.com -kube-url http://127.0.0.1:8001   # against `kubectl proxy`
//	portal                                             # in-cluster, behind an auth proxy (see deploy/portal)
//
// Every Kubernetes call impersonates the signed-in developer, so the portal
// can do exactly what `kubectl` lets that developer do, and nothing more.
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const version = "1.0.0"

//go:embed web
var webFS embed.FS

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	var (
		addr       = flag.String("addr", env("PORTAL_ADDR", ":8080"), "listen address")
		mock       = flag.Bool("mock", env("PORTAL_MOCK", "") == "true", "use an in-memory fake cluster (demo / UI development)")
		devUser    = flag.String("dev-user", env("PORTAL_DEV_USER", ""), "act as this user without an auth proxy (development only)")
		kubeURL    = flag.String("kube-url", env("KUBE_API_URL", ""), "Kubernetes API URL (default: in-cluster)")
		kubeToken  = flag.String("kube-token", env("KUBE_TOKEN", ""), "bearer token (default: service account token)")
		kubeCA     = flag.String("kube-ca", env("KUBE_CA_FILE", ""), "CA bundle for the API server")
		kubeInsec  = flag.Bool("kube-insecure", env("KUBE_INSECURE", "") == "true", "skip TLS verification (development only)")
		userHeader = flag.String("user-header", env("PORTAL_USER_HEADER", "X-Forwarded-User"), "header carrying the signed-in identity")
		grpHeader  = flag.String("groups-header", env("PORTAL_GROUPS_HEADER", "X-Forwarded-Groups"), "header carrying comma-separated groups")
		trusted    = flag.String("trusted-proxies", env("PORTAL_TRUSTED_PROXIES", "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"), "CIDRs allowed to set identity headers")
		afURL      = flag.String("artifactory-url", env("ARTIFACTORY_URL", ""), "Artifactory base URL (…/artifactory); default from the catalog")
		afToken    = flag.String("artifactory-token", env("ARTIFACTORY_TOKEN", ""), "token for Artifactory REST calls (read-only is enough)")
		afTTL      = flag.Duration("artifactory-cache", 5*time.Minute, "how long to cache Artifactory listings")
		labGroup   = flag.String("lab-group", env("PORTAL_LAB_GROUP", ""), "if set, only members of this group see hardware/kernel profiles")
		csp        = flag.String("csp", env("PORTAL_CSP", defaultCSP), "Content-Security-Policy header ('' to disable)")
		vendorDir  = flag.String("vendor-dir", env("PORTAL_VENDOR_DIR", ""), "directory with vendored fonts for air-gapped clusters (served at /vendor/)")
	)
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("portal: ")

	if *devUser == "" && *mock {
		*devUser = "demo@example.com"
	}
	if *devUser != "" {
		log.Printf("WARNING: -dev-user %s: identity is not verified; never expose this instance", *devUser)
	}

	s := &server{version: version, cats: &catalogCache{},
		ids: &identityResolver{devUser: *devUser, userHeader: *userHeader, grpHeader: *grpHeader, trusted: parseCIDRs(*trusted)}}

	var base *apiClient
	if *mock {
		mk := newMockKube(User{Name: *devUser, Short: sanitizeUser(*devUser)})
		s.kube = func(User) Kube { return mk }
		log.Printf("mock mode: in-memory cluster for %s", *devUser)
	} else {
		var err error
		cfg := kubeConfigFromEnv(*kubeURL, *kubeToken, *kubeCA)
		cfg.Insecure = *kubeInsec
		base, err = newAPIClient(cfg)
		if err != nil {
			log.Fatal(err)
		}
		s.kube = func(u User) Kube { return base.As(u) }
		log.Printf("kubernetes: %s (impersonating callers)", cfg.BaseURL)
	}

	// Artifactory: explicit flag/env wins, otherwise the catalog's URL.
	cat, source := s.cats.get(context.Background(), s.kube(User{Name: "system:portal-bootstrap", Short: "portal"}))
	log.Printf("catalog: %s (%d languages, %d tools)", source, len(cat.Languages), len(cat.Tools))
	url := *afURL
	if url == "" && !*mock {
		url = cat.Artifactory.URL
	}
	if url != "" {
		s.af = newArtifactory(url, *afToken, *afTTL)
		log.Printf("artifactory: %s", url)
	}

	s.canLab = func(ctx context.Context, k Kube, u User) bool {
		if *labGroup != "" && !contains(u.Groups, *labGroup) {
			return false
		}
		_, err := k.Get(ctx, "/api/v1/namespaces/"+u.Namespace("lab"))
		return err == nil
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", s.authenticate(s.routes()))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if base != nil {
			if _, err := base.Get(r.Context(), "/version"); err != nil {
				http.Error(w, "kubernetes unreachable: "+err.Error(), 503)
				return
			}
		}
		fmt.Fprintln(w, "ok")
	})
	if *vendorDir != "" {
		mux.Handle("/vendor/", http.StripPrefix("/vendor/", http.FileServer(http.Dir(*vendorDir))))
	}
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", spaHandler(http.FS(sub)))

	srv := &http.Server{Addr: *addr, Handler: logRequests(securityHeaders(*csp, mux)),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		log.Printf("listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// spaHandler serves the embedded site; unknown paths without a file extension
// fall back to index.html so hash-less deep links still load the app.
func spaHandler(root http.FileSystem) http.Handler {
	files := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := root.Open(p); err == nil {
			f.Close()
			if strings.HasSuffix(p, ".html") {
				w.Header().Set("Cache-Control", "no-cache")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			files.ServeHTTP(w, r)
			return
		}
		if !strings.Contains(p, ".") {
			r.URL.Path = "/"
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

// defaultCSP: scripts and styles only from the portal itself (Tailwind is
// pre-compiled and embedded), fonts from Google Fonts or /vendor/. No inline
// scripts or styles are used anywhere in the UI. Air-gapped clusters can drop
// the fonts.googleapis.com entries and vendor IBM Plex under -vendor-dir.
const defaultCSP = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com data:; " +
	"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
