package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// mockKube is an in-memory stand-in for the API server: enough behaviour for
// demos (`portal -mock`), UI development and tests. StatefulSets with
// replicas>0 grow a pod that becomes Ready a few seconds later; stopping
// removes it; secrets store stringData as data; logs are synthesised.
type mockKube struct {
	mu      sync.Mutex
	objects map[string]Obj // key: path
	created map[string]time.Time
	user    User
}

func newMockKube(user User) *mockKube {
	m := &mockKube{objects: map[string]Obj{}, created: map[string]time.Time{}, user: user}
	for _, ns := range []string{user.Namespace("standard"), user.Namespace("lab")} {
		m.objects["/api/v1/namespaces/"+ns] = Obj{"apiVersion": "v1", "kind": "Namespace",
			"metadata": Obj{"name": ns, "labels": Obj{"devspaces.io/tenant": user.Short}}}
	}
	m.objects[nsPath("", "v1", "configmaps", systemNamespace, catalogConfigMap)] = Obj{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": Obj{"name": catalogConfigMap, "namespace": systemNamespace},
		"data":     Obj{"catalog.json": string(defaultCatalogJSON)},
	}
	return m
}

func (m *mockKube) As(User) Kube { return m }

func (m *mockKube) tick() {
	// materialise / mature pods
	for p, o := range m.objects {
		if o["kind"] != "StatefulSet" {
			continue
		}
		ns := str(o, "metadata", "namespace")
		name := str(o, "metadata", "name")
		podPath := nsPath("", "v1", "pods", ns, name+"-0")
		replicas := num(o, "spec", "replicas")
		if replicas > 0 {
			if _, ok := m.objects[podPath]; !ok {
				tpl, _ := getIn(o, "spec", "template").(Obj)
				pod := Obj{"apiVersion": "v1", "kind": "Pod",
					"metadata": Obj{"name": name + "-0", "namespace": ns,
						"labels": getIn(tpl, "metadata", "labels"), "annotations": Obj{}},
					"spec":   deepCopy(getIn(tpl, "spec").(Obj)),
					"status": Obj{"phase": "Pending", "startTime": time.Now().UTC().Format(time.RFC3339), "conditions": []any{}},
				}
				pod["spec"].(Obj)["nodeName"] = "node-1"
				m.objects[podPath] = pod
				m.created[podPath] = time.Now()
			}
			pod := m.objects[podPath]
			if time.Since(m.created[podPath]) > 4*time.Second && str(pod, "status", "phase") != "Running" {
				pod["status"] = Obj{"phase": "Running", "startTime": str(pod, "status", "startTime"),
					"conditions":        []any{Obj{"type": "PodScheduled", "status": "True"}, Obj{"type": "Ready", "status": "True"}},
					"containerStatuses": []any{Obj{"name": "workspace", "ready": true, "state": Obj{"running": Obj{}}}}}
				pod["metadata"].(Obj)["annotations"].(Obj)["devspaces.io/last-active"] = time.Now().UTC().Format(time.RFC3339)
			}
			ready := 0.0
			if str(pod, "status", "phase") == "Running" {
				ready = 1
			}
			o["status"] = Obj{"readyReplicas": ready, "replicas": replicas}
		} else {
			delete(m.objects, podPath)
			o["status"] = Obj{"readyReplicas": 0.0, "replicas": 0.0}
		}
		_ = p
	}
}

func (m *mockKube) Get(_ context.Context, path string) (Obj, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tick()
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	o, ok := m.objects[path]
	if !ok {
		return nil, errNotFound
	}
	return deepCopy(o), nil
}

func (m *mockKube) List(_ context.Context, path, selector string) ([]Obj, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tick()
	want := map[string]string{}
	keys := []string{}
	for _, part := range strings.Split(selector, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		if k, v, ok := strings.Cut(part, "="); ok {
			want[k] = v
		} else {
			keys = append(keys, part)
		}
	}
	var out []Obj
	var names []string
	for p := range m.objects {
		if strings.HasPrefix(p, path+"/") && !strings.Contains(p[len(path)+1:], "/") {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	for _, p := range names {
		o := m.objects[p]
		l := labels(o)
		ok := true
		for k, v := range want {
			ok = ok && l[k] == v
		}
		for _, k := range keys {
			_, has := l[k]
			ok = ok && has
		}
		if ok {
			out = append(out, deepCopy(o))
		}
	}
	return out, nil
}

func (m *mockKube) Apply(_ context.Context, path string, obj Obj) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := deepCopy(obj)
	if o["kind"] == "Secret" {
		if sd, ok := o["stringData"].(Obj); ok {
			data := Obj{}
			if old, ok := m.objects[path]; ok {
				if od, ok := old["data"].(Obj); ok {
					for k, v := range od {
						data[k] = v
					}
				}
			}
			for k, v := range sd {
				data[k] = base64.StdEncoding.EncodeToString([]byte(fmt.Sprint(v)))
			}
			o["data"] = data
			delete(o, "stringData")
		}
	}
	if o["kind"] == "PersistentVolumeClaim" {
		o["status"] = Obj{"phase": "Bound", "capacity": Obj{"storage": str(o, "spec", "resources", "requests", "storage")}}
	}
	if o["kind"] == "Service" && str(o, "spec", "type") == "LoadBalancer" {
		o["status"] = Obj{"loadBalancer": Obj{"ingress": []any{Obj{"ip": "203.0.113.42"}}}}
	}
	if old, ok := m.objects[path]; ok && o["kind"] == "StatefulSet" {
		// keep replicas if the apply doesn't change them (mirrors a scale subresource)
		if _, has := getIn(o, "spec").(Obj)["replicas"]; !has {
			o["spec"].(Obj)["replicas"] = num(old, "spec", "replicas")
		}
	}
	m.objects[path] = o
	m.tick()
	return nil
}

func (m *mockKube) MergePatch(_ context.Context, path string, patch Obj) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	path = strings.TrimSuffix(path, "/scale")
	o, ok := m.objects[path]
	if !ok {
		return errNotFound
	}
	merge(o, patch)
	if sd, ok := o["stringData"].(Obj); ok {
		data, _ := o["data"].(Obj)
		if data == nil {
			data = Obj{}
		}
		for k, v := range sd {
			data[k] = base64.StdEncoding.EncodeToString([]byte(fmt.Sprint(v)))
		}
		o["data"] = data
		delete(o, "stringData")
	}
	m.tick()
	return nil
}

func merge(dst, src Obj) {
	for k, v := range src {
		if sv, ok := v.(Obj); ok {
			if dv, ok := dst[k].(Obj); ok {
				merge(dv, sv)
				continue
			}
		}
		if v == nil {
			delete(dst, k)
			continue
		}
		dst[k] = v
	}
}

func (m *mockKube) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, path)
	m.tick()
	return nil
}

func (m *mockKube) Logs(_ context.Context, ns, pod, _ string, _ int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[nsPath("", "v1", "pods", ns, pod)]; !ok {
		return "", errNotFound
	}
	return strings.Join([]string{
		"[workspace 10:00:01] workspace 'demo' starting: lang=go profile=standard tools=ripgrep,cmake",
		"[workspace 10:00:01] first boot: seeding /home/dev from /opt/workspace/skel",
		"[workspace 10:00:02] sshd listening on 127.0.0.1:2222 (pid 42)",
		"[workspace 10:00:02] configuring package managers for Artifactory at https://artifactory.example.com/artifactory",
		"[workspace 10:00:05] installing artifact tool: ripgrep",
		"[workspace 10:00:07] tool ready: ripgrep",
		"[workspace 10:00:07] installing tool: cmake",
		"[workspace 10:00:21] tool ready: cmake",
		"[workspace 10:00:21] cloning git@github.com:acme/engine.git into /home/dev/workspace/engine",
		"[workspace 10:00:26] clone complete",
		"[workspace 10:00:26] ready",
	}, "\n"), nil
}
