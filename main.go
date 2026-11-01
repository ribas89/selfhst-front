package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

var supported = map[string]bool{"svg": true, "png": true, "webp": true, "avif": true, "ico": true}
var styleOrder = map[string]int{"default": 0, "dark": 1, "light": 2}
var formatOrder = map[string]int{"svg": 0, "png": 1, "webp": 2, "avif": 3, "ico": 4}

type Variant struct {
	Style  string `json:"style"`
	Format string `json:"format"`
	URL    string `json:"url"`
}
type Icon struct {
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Category  string    `json:"category,omitempty"`
	CreatedAt string    `json:"createdAt,omitempty"`
	Variants  []Variant `json:"variants"`
}
type Catalog struct {
	Items     []Icon `json:"items"`
	Total     int    `json:"total"`
	ScannedAt string `json:"scannedAt"`
	Message   string `json:"message,omitempty"`
}
type Gallery struct {
	root    string
	ttl     time.Duration
	mu      sync.Mutex
	catalog Catalog
	expires time.Time
}

type Meta struct {
	Name      string
	Category  string
	CreatedAt string
}

func main() {
	check := flag.Bool("healthcheck", false, "check HTTP health endpoint")
	flag.Parse()
	port := getenv("PORT", "8080")
	host := getenv("HOST", "0.0.0.0")
	if *check {
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
		if err != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}

	upstreamURL := getenv("ICONS_UPSTREAM", "http://icons:80")
	target, err := url.Parse(upstreamURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		log.Fatalf("Invalid ICONS_UPSTREAM %q", upstreamURL)
	}
	root := getenv("ICONS_DIR", "/icons")
	gallery := &Gallery{root: root, ttl: 60 * time.Second}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Icon API unavailable", http.StatusBadGateway)
		log.Printf("proxy error %s: %v", r.URL.Path, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		ioWriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /api/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		cat := gallery.get(r.URL.Query().Get("refresh") == "1")
		if err := json.NewEncoder(w).Encode(cat); err != nil {
			log.Printf("JSON encoding error: %v", err)
		}
	})
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}
	files := http.FileServer(http.FS(sub))
	mux.Handle("GET /static/", http.StripPrefix("/static/", files))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			proxy.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "UI unavailable", 500)
			return
		}
		_, _ = w.Write(data)
	})
	// The original selfh.st icon routes and /health are sent to the existing API.
	mux.HandleFunc("/", proxy.ServeHTTP)
	server := &http.Server{
		Addr:              host + ":" + port,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       45 * time.Second,
	}
	log.Printf("icon gallery listening at %s, catalog %s, icon API %s", server.Addr, root, upstreamURL)
	log.Fatal(server.ListenAndServe())
}

func ioWriteString(w http.ResponseWriter, s string) { _, _ = w.Write([]byte(s)) }
func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (g *Gallery) get(refresh bool) Catalog {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !refresh && time.Now().Before(g.expires) {
		return g.catalog
	}
	cat, err := scan(g.root)
	if err != nil {
		cat.Message = "Unable to scan icon directory: " + err.Error()
	}
	cat.Total = len(cat.Items)
	cat.ScannedAt = time.Now().UTC().Format(time.RFC3339)
	if cat.Total == 0 && cat.Message == "" {
		cat.Message = "No icons found. Mount the repository root containing svg/, png/, webp/, avif/, ico/ at ICONS_DIR. The build/ source folder by itself does not contain icons."
	}
	g.catalog = cat
	g.expires = time.Now().Add(g.ttl)
	return cat
}

func scan(root string) (Catalog, error) {
	cat := Catalog{Items: []Icon{}}
	info, err := os.Stat(root)
	if err != nil {
		return cat, err
	}
	if !info.IsDir() {
		return cat, errors.New("ICONS_DIR is not a directory")
	}
	metadata := loadMetadata(root)
	grouped := map[string]*Icon{}
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		depth := strings.Count(filepath.ToSlash(rel), "/")
		if entry.IsDir() {
			if depth >= 5 {
				return filepath.SkipDir
			}
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), ".")
		if !supported[ext] || !strings.EqualFold(filepath.Base(filepath.Dir(path)), ext) {
			return nil
		}
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if stem == "" || strings.ContainsAny(stem, "\\/") {
			return nil
		}
		style := "default"
		slug := stem
		for _, suffix := range []string{"-dark", "-light"} {
			if strings.HasSuffix(strings.ToLower(stem), suffix) {
				style = strings.TrimPrefix(suffix, "-")
				slug = stem[:len(stem)-len(suffix)]
				break
			}
		}
		if slug == "" {
			return nil
		}
		key := strings.ToLower(slug)
		item, ok := grouped[key]
		if !ok {
			label := labelFor(slug)
			category, createdAt := "", ""
			if meta, ok := metadata[key]; ok {
				if meta.Name != "" {
					label = meta.Name
				}
				category, createdAt = meta.Category, meta.CreatedAt
			}
			item = &Icon{Name: label, Slug: slug, Category: category, CreatedAt: createdAt, Variants: []Variant{}}
			grouped[key] = item
		}
		vkey := fmt.Sprintf("%s|%s|%s", key, style, ext)
		if seen[vkey] {
			return nil
		}
		seen[vkey] = true
		u := "/" + url.PathEscape(stem+"."+ext)
		item.Variants = append(item.Variants, Variant{Style: style, Format: ext, URL: u})
		return nil
	})
	if err != nil {
		return cat, err
	}
	for _, icon := range grouped {
		sort.Slice(icon.Variants, func(i, j int) bool {
			a, b := icon.Variants[i], icon.Variants[j]
			if a.Style != b.Style {
				return styleOrder[a.Style] < styleOrder[b.Style]
			}
			return formatOrder[a.Format] < formatOrder[b.Format]
		})
		cat.Items = append(cat.Items, *icon)
	}
	sort.Slice(cat.Items, func(i, j int) bool { return strings.ToLower(cat.Items[i].Name) < strings.ToLower(cat.Items[j].Name) })
	return cat, nil
}

func labelFor(slug string) string {
	return strings.Join(strings.FieldsFunc(strings.ReplaceAll(slug, "_", "-"), func(r rune) bool { return r == '-' }), " ")
}

// The repository metadata is mounted at /icons (./icons/build on the host).
// index-consolidated.json provides display labels; index.json provides CreatedAt.
 // Variants always come from the actual local image files.
func loadMetadata(root string) map[string]Meta {
	meta := map[string]Meta{}
	roots := []string{root}
	if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != ".git" {
				roots = append(roots, filepath.Join(root, entry.Name()))
			}
		}
	}
	// Prefer the consolidated index for names and categories when available.
	for _, dir := range roots {
		contents, err := os.ReadFile(filepath.Join(dir, "index-consolidated.json"))
		if err != nil { continue }
		var rows [][]any
		if json.Unmarshal(contents, &rows) != nil { continue }
		for _, row := range rows {
			if len(row) < 2 { continue }
			name, okName := row[0].(string)
			slug, okSlug := row[1].(string)
			if !okName || !okSlug || slug == "" { continue }
			category := ""
			if len(row) > 7 { category, _ = row[7].(string) }
			meta[strings.ToLower(slug)] = Meta{Name: name, Category: category}
		}
		break
	}
	// CreatedAt is the upstream icon-added timestamp, not the file's local mtime.
	// Don't guess a timestamp if an icon is absent from the index.
	for _, dir := range roots {
		contents, err := os.ReadFile(filepath.Join(dir, "index.json"))
		if err != nil { continue }
		var rows []struct {
			Name string `json:"Name"`
			Reference string `json:"Reference"`
			Category string `json:"Category"`
			CreatedAt string `json:"CreatedAt"`
		}
		if json.Unmarshal(contents, &rows) != nil { continue }
		for _, row := range rows {
			if row.Reference == "" { continue }
			key := strings.ToLower(row.Reference)
			m := meta[key]
			if m.Name == "" { m.Name = row.Name }
			if m.Category == "" { m.Category = row.Category }
			if row.CreatedAt != "" {
				date := strings.Replace(row.CreatedAt, " ", "T", 1)
				if ts, err := time.Parse(time.RFC3339Nano, date); err == nil {
					m.CreatedAt = ts.UTC().Format(time.RFC3339Nano)
				}
			}
			meta[key] = m
		}
		break
	}
	return meta
}
