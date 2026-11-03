package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	systemNamespace  = "devspaces-system"
	catalogConfigMap = "workspace-catalog"
)

// The catalog is the single source of truth shared with the CLI. The live copy
// is the ConfigMap; this embedded copy (deploy/platform/catalog.json, copied by
// build.sh) is the fallback when the ConfigMap cannot be read.
//
//go:embed catalog.default.json
var defaultCatalogJSON []byte

// Catalog is decoded loosely: the UI shows whatever admins put in, the renderer
// reads the fields it knows.
type Catalog struct {
	Registry    string            `json:"registry"`
	Tag         string            `json:"tag"`
	PullSecret  string            `json:"pullSecret"`
	Artifactory ArtifactoryConfig `json:"artifactory"`
	Defaults    map[string]string `json:"defaults"`
	Languages   map[string]Obj    `json:"languages"`
	Tools       map[string]Obj    `json:"tools"`
	Profiles    map[string]Obj    `json:"profiles"`
	Isolation   map[string]Obj    `json:"isolation"`
	Devices     map[string]Obj    `json:"devices"`
	Networking  Obj               `json:"networking"`
	Storage     StorageConfig     `json:"storage"`
	raw         Obj
}

// StorageConfig mirrors catalog.json "storage": what developers may pick for home and
// shared drives, and how snapshots are taken.
type StorageConfig struct {
	Choices           []StorageChoice `json:"choices"`
	SnapshotClass     string          `json:"snapshotClass"`
	ProtectHomeDrives bool            `json:"protectHomeDrives"`
	SharedMountPath   string          `json:"sharedMountPath"`
}

type StorageChoice struct {
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	Kind         string   `json:"kind"` // home | shared
	StorageClass string   `json:"storageClass"`
	AccessModes  []string `json:"accessModes"`
	Description  string   `json:"description"`
	Default      bool     `json:"default"`
}

// StorageChoice resolves a choice name (or the default) of the given kind.
func (c *Catalog) StorageChoice(name, kind string) (StorageChoice, error) {
	var first *StorageChoice
	for i := range c.Storage.Choices {
		ch := &c.Storage.Choices[i]
		if orDefault(ch.Kind, "home") != kind {
			continue
		}
		if name != "" && ch.Name == name {
			return *ch, nil
		}
		if name == "" && ch.Default {
			return *ch, nil
		}
		if first == nil {
			first = ch
		}
	}
	if name != "" {
		return StorageChoice{}, invalid("unknown %s storage %q", kind, name)
	}
	if first != nil {
		return *first, nil
	}
	return StorageChoice{Kind: kind, AccessModes: []string{ifThen(kind == "shared", "ReadWriteMany", "ReadWriteOnce")}}, nil
}

// MeshMode is networking.mesh.mode (none | istio-ambient).
func (c *Catalog) MeshMode() string { return orDefault(str(c.Networking, "mesh", "mode"), "none") }

// LinksEnabled is networking.links.enabled (default true).
func (c *Catalog) LinksEnabled() bool {
	if l, ok := getIn(c.Networking, "links", "enabled").(bool); ok {
		return l
	}
	return true
}

type ArtifactoryConfig struct {
	URL     string            `json:"url"`
	Repos   map[string]string `json:"repos"`
	Mirrors map[string]string `json:"mirrors"`
	Docs    string            `json:"docs"`
}

func parseCatalog(b []byte) (*Catalog, error) {
	var raw Obj
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	stripComments(raw) // "$comment" keys are documentation for humans, not data
	clean, _ := json.Marshal(raw)
	var c Catalog
	if err := json.Unmarshal(clean, &c); err != nil {
		return nil, err
	}
	c.raw = raw
	if c.Defaults == nil {
		c.Defaults = map[string]string{}
	}
	return &c, nil
}

func stripComments(o Obj) {
	for k, v := range o {
		if strings.HasPrefix(k, "$") {
			delete(o, k)
			continue
		}
		if m, ok := v.(Obj); ok {
			stripComments(m)
		}
	}
}

func (c *Catalog) Default(k, fallback string) string {
	if v := c.Defaults[k]; v != "" {
		return v
	}
	return fallback
}

// Public returns the catalog as the UI sees it, with sorted key lists so the
// front end can render deterministic menus without caring about map order.
func (c *Catalog) Public() Obj {
	out := deepCopy(c.raw)
	for _, k := range []string{"languages", "tools", "profiles", "isolation", "devices"} {
		m, _ := out[k].(Obj)
		keys := make([]string, 0, len(m))
		for key := range m {
			if !strings.HasPrefix(key, "$") {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		out[k+"Order"] = keys
	}
	return out
}

// catalogCache reads the ConfigMap at most once a minute.
type catalogCache struct {
	mu      sync.Mutex
	cat     *Catalog
	source  string
	fetched time.Time
}

func (cc *catalogCache) get(ctx context.Context, k Kube) (*Catalog, string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.cat != nil && time.Since(cc.fetched) < time.Minute {
		return cc.cat, cc.source
	}
	if k != nil {
		cm, err := k.Get(ctx, nsPath("", "v1", "configmaps", systemNamespace, catalogConfigMap))
		if err == nil {
			if raw := str(cm, "data", "catalog.json"); raw != "" {
				if cat, err := parseCatalog([]byte(raw)); err == nil {
					cc.cat, cc.source, cc.fetched = cat, "cluster", time.Now()
					return cat, cc.source
				}
			}
		}
	}
	if cc.cat == nil {
		cat, err := parseCatalog(defaultCatalogJSON)
		if err != nil {
			panic("embedded catalog is invalid: " + err.Error())
		}
		cc.cat, cc.source = cat, "embedded"
	}
	cc.fetched = time.Now()
	return cc.cat, cc.source
}

// ------------------------------------------------------------------ durations

var durRE = regexp.MustCompile(`^(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$`)

// parseDuration accepts the CLI's forms: 30m, 4h, 1d12h, off/0/never.
func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "0", "off", "none", "never":
		return 0, nil
	}
	m := durRE.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "" && m[3] == "" && m[4] == "") {
		return 0, fmt.Errorf("bad duration %q (examples: 30m, 4h, 1d12h, off)", s)
	}
	var d time.Duration
	for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute, time.Second} {
		if m[i+1] != "" {
			var n int
			fmt.Sscanf(m[i+1], "%d", &n)
			d += time.Duration(n) * unit
		}
	}
	return d, nil
}

var quantityRE = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(m|k|M|G|T|Ki|Mi|Gi|Ti)?$`)
var nameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,38}[a-z0-9])?$`)
