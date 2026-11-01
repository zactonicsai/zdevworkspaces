package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// artifactory talks to JFrog Artifactory's REST API to turn "what is in our
// registry" into choices on the Create page:
//   - workspace images and their tags  (Docker repo:  /api/docker/{repo}/v2/_catalog, /v2/{image}/tags/list)
//   - whether each catalog tool's artifact exists (HEAD on the resolved URL)
//   - versions of tools kept in a local generic repo (/api/storage/{repo}/{path}?list)
//
// Results are cached (default 5 min). With no URL configured, or on any error,
// the portal falls back to the static catalog so the UI always works.
type artifactory struct {
	base  string // https://host/artifactory
	token string
	http  *http.Client
	ttl   time.Duration

	mu     sync.Mutex
	images cached[map[string][]string]
	tools  cached[map[string]toolAvailability]
}

type cached[T any] struct {
	val T
	at  time.Time
	err error
}

type toolAvailability struct {
	Available bool     `json:"available"`
	URL       string   `json:"url,omitempty"`
	Versions  []string `json:"versions,omitempty"`
	Checked   string   `json:"checked,omitempty"`
	Error     string   `json:"error,omitempty"`
}

func newArtifactory(base, token string, ttl time.Duration) *artifactory {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &artifactory{base: strings.TrimRight(base, "/"), token: token, ttl: ttl,
		http: &http.Client{Timeout: 20 * time.Second}}
}

func (a *artifactory) enabled() bool { return a != nil && a.base != "" }

func (a *artifactory) req(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, nil)
	if err != nil {
		return nil, err
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("Accept", "application/json")
	return a.http.Do(req)
}

func (a *artifactory) getJSON(ctx context.Context, path string, out any) error {
	res, err := a.req(ctx, http.MethodGet, path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("artifactory %s: HTTP %d %s", path, res.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// Images lists workspace-* repositories and tags in the Docker repo.
func (a *artifactory) Images(ctx context.Context, repo string, prefix string) (map[string][]string, error) {
	a.mu.Lock()
	if a.images.val != nil && time.Since(a.images.at) < a.ttl {
		defer a.mu.Unlock()
		return a.images.val, a.images.err
	}
	a.mu.Unlock()

	out := map[string][]string{}
	var cat struct {
		Repositories []string `json:"repositories"`
	}
	err := a.getJSON(ctx, "/api/docker/"+url.PathEscape(repo)+"/v2/_catalog?n=1000", &cat)
	if err == nil {
		for _, img := range cat.Repositories {
			if prefix != "" && !strings.HasPrefix(img, prefix) {
				continue
			}
			var tags struct {
				Tags []string `json:"tags"`
			}
			if e := a.getJSON(ctx, "/api/docker/"+url.PathEscape(repo)+"/v2/"+img+"/tags/list", &tags); e == nil {
				sort.Sort(sort.Reverse(sort.StringSlice(tags.Tags)))
				out[img] = tags.Tags
			}
		}
	}
	a.mu.Lock()
	a.images = cached[map[string][]string]{val: out, at: time.Now(), err: err}
	a.mu.Unlock()
	return out, err
}

// Tools checks each artifact-backed catalog tool: does its download resolve,
// and (for local generic repos) which other versions sit next to it.
func (a *artifactory) Tools(ctx context.Context, cat *Catalog) map[string]toolAvailability {
	a.mu.Lock()
	if a.tools.val != nil && time.Since(a.tools.at) < a.ttl {
		defer a.mu.Unlock()
		return a.tools.val
	}
	a.mu.Unlock()

	out := map[string]toolAvailability{}
	names := make([]string, 0, len(cat.Tools))
	for n := range cat.Tools {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, spec := range artifactSpecs(cat, names) {
		name := str(spec, "name")
		u := strings.NewReplacer("{arch}", "x86_64", "{goarch}", "amd64").Replace(str(spec, "url"))
		ta := toolAvailability{URL: u, Checked: time.Now().UTC().Format(time.RFC3339)}
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		req, _ := http.NewRequestWithContext(cctx, http.MethodHead, u, nil)
		if a.token != "" {
			req.Header.Set("Authorization", "Bearer "+a.token)
		}
		res, err := a.http.Do(req)
		cancel()
		if err != nil {
			ta.Error = err.Error()
		} else {
			res.Body.Close()
			ta.Available = res.StatusCode < 300
			if !ta.Available {
				ta.Error = fmt.Sprintf("HTTP %d", res.StatusCode)
			}
		}
		// versions: only meaningful for a local/virtual repo laid out as <tool>/<version>/…
		if art, _ := cat.Tools[name]["artifact"].(Obj); art != nil && str(art, "repo") == "generic" {
			repo := cat.Artifactory.Repos["generic"]
			dir := str(art, "path")
			if i := strings.Index(dir, "{version}"); i > 0 {
				dir = strings.TrimRight(dir[:i], "/")
				var listing struct {
					Children []struct {
						URI    string `json:"uri"`
						Folder bool   `json:"folder"`
					} `json:"children"`
				}
				if e := a.getJSON(ctx, "/api/storage/"+url.PathEscape(repo)+"/"+dir, &listing); e == nil {
					for _, c := range listing.Children {
						if c.Folder {
							ta.Versions = append(ta.Versions, strings.TrimPrefix(c.URI, "/"))
						}
					}
					sort.Sort(sort.Reverse(sort.StringSlice(ta.Versions)))
				}
			}
		}
		out[name] = ta
	}
	a.mu.Lock()
	a.tools = cached[map[string]toolAvailability]{val: out, at: time.Now()}
	a.mu.Unlock()
	return out
}
