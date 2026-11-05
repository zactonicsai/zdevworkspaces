# Helm Charts Tutorial for Beginners
## Explained Simply — Middle School Style (Updated November 2026)

**What you will learn:**
- What Kubernetes and Helm are, and why Helm makes life easier
- How to build your **first chart by hand**, step by step (Part 1)
- Every file inside a chart and what it does
- **How to write templates** — the `{{ }}` language, explained with lots of tiny examples
- How to build a complete, "real" chart one piece at a time
- How to test and debug charts before you break anything
- How to add login with **Keycloak** and traffic control with **Istio**
- Advanced tricks: subcharts, hooks, library charts, secrets, GitOps
- Best practices, common mistakes, pros and cons, and practice exercises

> **What's new in this version?**
> - **Helm 4** came out in November 2025. Everything here works with Helm 4. Charts still use `apiVersion: v2`, so old charts keep working.
> - **Ingress NGINX was retired in March 2026.** The modern "front door" for Kubernetes is the **Gateway API**, so this tutorial teaches that first.
> - Istio examples use the stable `networking.istio.io/v1` API and explain **ambient mode** (Istio without sidecars).
> - The Keycloak example was fixed to use the current `keycloakx` chart settings.

---

## Table of Contents

1. [Part 1 — Quick Start: Build Your First Chart Step by Step](#part-1--quick-start-build-your-first-chart-step-by-step)
2. [Part 2 — The Big Picture (Background)](#part-2--the-big-picture-background)
3. [Part 3 — What's Inside a Chart](#part-3--whats-inside-a-chart)
4. [Part 4 — How to Write Templates (The `{{ }}` Language)](#part-4--how-to-write-templates-the---language)
5. [Part 5 — Writing a Complete Chart, Piece by Piece](#part-5--writing-a-complete-chart-piece-by-piece)
6. [Part 6 — Values: Turning the Knobs](#part-6--values-turning-the-knobs)
7. [Part 7 — Testing and Debugging](#part-7--testing-and-debugging)
8. [Part 8 — Adding Login with Keycloak](#part-8--adding-login-with-keycloak)
9. [Part 9 — Traffic Control and Security with Istio](#part-9--traffic-control-and-security-with-istio)
10. [Part 10 — Advanced Examples](#part-10--advanced-examples)
11. [Part 11 — Pros and Cons: Helm vs. Other Tools](#part-11--pros-and-cons-helm-vs-other-tools)
12. [Part 12 — Best Practices Checklist](#part-12--best-practices-checklist)
13. [Part 13 — Common Mistakes (and Fixes)](#part-13--common-mistakes-and-fixes)
14. [Part 14 — Command Cheat Sheet](#part-14--command-cheat-sheet)
15. [Part 15 — Glossary](#part-15--glossary)
16. [Part 16 — Practice Exercises (with Answers)](#part-16--practice-exercises-with-answers)
17. [Part 17 — Next Steps](#part-17--next-steps)

---

# Part 1 — Quick Start: Build Your First Chart Step by Step

Before we learn all the details, let's build something that works. We will write a tiny chart **by hand** (no shortcuts) that runs a web page saying "Hello from Helm!"

When you finish, you will have done the 5 most important Helm things: **create, install, upgrade, roll back, uninstall.**

### Step 0: What you need

| Tool | What it is | How to get it |
|------|-----------|---------------|
| **Docker** (or Podman) | Runs containers on your computer | docker.com |
| **kind** | Makes a mini Kubernetes cluster inside Docker | `brew install kind` (Mac) or see kind.sigs.k8s.io |
| **kubectl** | The remote control for Kubernetes | `brew install kubectl` |
| **Helm** | The chart tool | See below |

**Install Helm:**
```bash
# Mac
brew install helm

# Linux (official script for Helm 4)
curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-4 | bash

# Windows
winget install Helm.Helm

# Check it worked
helm version
```

**Make a practice cluster:**
```bash
kind create cluster --name helm-school
kubectl get nodes        # you should see one node that says "Ready"
```

> 💡 Using Docker Desktop's built-in Kubernetes, minikube, or Rancher Desktop also works. Anything with a working `kubectl` is fine.

### Step 1: Make the folders

```bash
mkdir -p hello-web/templates
cd hello-web
```

### Step 2: Write `Chart.yaml` (the name tag)

Create `hello-web/Chart.yaml`:
```yaml
apiVersion: v2              # Chart format version. Always v2 for now.
name: hello-web             # The chart's name
description: My very first Helm chart
type: application
version: 0.1.0              # Version of the CHART (the recipe)
appVersion: "1.29"          # Version of the APP inside (the food)
```

### Step 3: Write `values.yaml` (the knobs)

Create `hello-web/values.yaml`:
```yaml
replicaCount: 1

message: "Hello from Helm!"

image:
  repository: nginx
  tag: "1.29"

service:
  port: 80
```

### Step 4: Write the templates (the instructions with blanks)

**`templates/configmap.yaml`** — holds our web page:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-page
data:
  index.html: |
    <h1>{{ .Values.message }}</h1>
    <p>I am release "{{ .Release.Name }}" running {{ .Values.replicaCount }} copy/copies.</p>
```

**`templates/deployment.yaml`** — runs the app:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app: {{ .Release.Name }}
    spec:
      containers:
        - name: web
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          ports:
            - containerPort: 80
          volumeMounts:
            - name: page
              mountPath: /usr/share/nginx/html
      volumes:
        - name: page
          configMap:
            name: {{ .Release.Name }}-page
```

**`templates/service.yaml`** — the hallway sign so people can find the app:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}
spec:
  selector:
    app: {{ .Release.Name }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: 80
```

Your folder now looks like this:
```
hello-web/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── configmap.yaml
    ├── deployment.yaml
    └── service.yaml
```

### Step 5: Check your work before installing

```bash
cd ..                              # go back to the folder that holds hello-web/
helm lint ./hello-web              # looks for mistakes
helm template demo ./hello-web     # shows the finished YAML (nothing is installed)
```

In the `helm template` output, every `{{ }}` has been replaced. For example, `{{ .Release.Name }}` became `demo`.

### Step 6: Install it

```bash
helm install demo ./hello-web
kubectl get pods                   # wait until STATUS says Running
```

### Step 7: Visit your web page

```bash
kubectl port-forward svc/demo 8080:80
```
Open http://localhost:8080 in your browser. You should see **Hello from Helm!** 🎉
(Press `Ctrl+C` to stop port-forwarding.)

### Step 8: Upgrade it (change a knob)

```bash
helm upgrade demo ./hello-web --set message="Helm is awesome!" --set replicaCount=2
helm history demo          # see revision 1 and revision 2
```

> ⚠️ The page might still show the old message. Why? nginx pods don't notice when a ConfigMap changes. In Part 5 you'll learn the **checksum trick** that fixes this automatically. For now, run:
> `kubectl rollout restart deployment demo`

### Step 9: Oops! Roll it back

```bash
helm rollback demo 1       # go back to revision 1
helm history demo          # now there's a revision 3 that copies revision 1
```

### Step 10: Clean up

```bash
helm uninstall demo
kind delete cluster --name helm-school   # only if you're done practicing
```

**🏆 You did it!** You wrote a chart from nothing, installed it, changed it, undid the change, and removed it. Everything else in this tutorial builds on these same steps.

---

# Part 2 — The Big Picture (Background)

## 2.1 What is Kubernetes? (The School Analogy)

Imagine your computer cluster (Kubernetes) is a **big school**.

| Kubernetes word | School version | What it really is |
|-----------------|---------------|-------------------|
| **Cluster** | The whole school campus | A group of computers working together |
| **Node** | A school building | One computer in the cluster |
| **Pod** | A classroom | Where your app actually runs |
| **Container** | A student's lunchbox | Your app plus everything it needs, packed together |
| **Deployment** | The rule "always keep 3 math classrooms open" | Keeps the right number of Pods running |
| **Service** | The hallway sign "Math → Room 204" | A steady address to find Pods |
| **ConfigMap** | The schedule posted on the wall | Normal settings |
| **Secret** | The locked locker | Passwords and keys |
| **Namespace** | A grade level (6th, 7th, 8th) | A way to group and separate things |
| **Gateway / Ingress** | The front door of the school | How the outside world gets in |

## 2.2 What problem does Helm solve?

To run one app, you might need 5–10 YAML files. Now imagine:
- You need the same app in **dev**, **test**, and **production**, each a little different.
- You need to update it every week.
- Something breaks and you need to undo the update **fast**.

Copying and editing YAML files by hand for all of that is boring and easy to mess up.

**Helm is like a smart recipe book.**
- The **chart** is the recipe.
- The **values** are the ingredients you can swap (more cheese, less salt).
- A **release** is one meal you cooked from the recipe.
- Helm remembers every meal you cooked, so you can **go back** to an earlier one.

Helm is often called **"the package manager for Kubernetes,"** the same way an app store installs apps on your phone.

## 2.3 A tiny bit of history

- **2015:** Helm was born at a hackathon.
- **2019:** **Helm 3** removed a server part called "Tiller," making Helm simpler and safer.
- **November 2025:** **Helm 4** was released — the first big new version in 6 years. It added a new plugin system (including WebAssembly plugins), server-side apply, and better logging.
- Helm 3 is reaching its end of support during 2026, so **new learners should use Helm 4.**

> 💡 Good news: almost everything you learn here works the same in Helm 3 and Helm 4. Charts with `apiVersion: v2` work in both.

## 2.4 Key words you'll see everywhere

| Word | Meaning |
|------|---------|
| **Chart** | A folder (or `.tgz` package) with templates + default values |
| **Values** | The settings that fill in the blanks |
| **Template** | A YAML file with blanks like `{{ .Values.replicaCount }}` |
| **Release** | One installed copy of a chart, with a name you choose |
| **Revision** | A numbered version of a release (1, 2, 3...). Every upgrade or rollback adds one |
| **Repository** | A place to download charts from (like an app store) |
| **OCI registry** | A container registry that can also store charts (the modern way) |

---

# Part 3 — What's Inside a Chart

Running `helm create my-webapp` makes a starter chart with good habits built in:

```
my-webapp/
├── Chart.yaml            ← Name tag: name, version, description, dependencies
├── values.yaml           ← Default knobs
├── values.schema.json    ← (optional, you add it) Rules that check the knobs
├── charts/               ← Other charts this one needs (subcharts)
├── templates/            ← Kubernetes files with blanks
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── serviceaccount.yaml
│   ├── ingress.yaml
│   ├── hpa.yaml          ← Autoscaler (adds more copies when busy)
│   ├── NOTES.txt         ← Message printed after install
│   ├── _helpers.tpl      ← Reusable snippets ("helper functions")
│   └── tests/
│       └── test-connection.yaml
├── README.md             ← (you add it) Explains your values
└── .helmignore           ← Files Helm should NOT pack up (like .gitignore)
```

### The rules Helm follows for the `templates/` folder
- Files ending in `.yaml`, `.yml`, or `.tpl` are templates.
- Files starting with an **underscore** (`_helpers.tpl`) are **not** turned into Kubernetes objects. They only hold reusable snippets.
- `NOTES.txt` is not sent to Kubernetes. It's printed on the screen after `helm install`.

### `Chart.yaml` — every field explained
```yaml
apiVersion: v2                     # Chart format. Use v2.
name: my-webapp                    # Lowercase letters, numbers, dashes
description: A simple web app that says hello
type: application                  # "application" (installable) or "library" (helpers only)
version: 1.0.0                     # CHART version — change this every time you change the chart
appVersion: "1.0.0"                # APP version — just a label, in quotes
kubeVersion: ">=1.29.0-0"          # (optional) Which Kubernetes versions are allowed
keywords: [web, demo]              # (optional) Helps people find it
home: https://example.com          # (optional)
maintainers:                       # (optional)
  - name: Your Name
    email: you@example.com
dependencies: []                   # (optional) Subcharts — see Part 10
```

> 📏 **Version numbers use SemVer:** `MAJOR.MINOR.PATCH`.
> - Fixed a typo? `1.0.0 → 1.0.1` (patch)
> - Added a new feature? `1.0.1 → 1.1.0` (minor)
> - Changed things so old values break? `1.1.0 → 2.0.0` (major)

---

# Part 4 — How to Write Templates (The `{{ }}` Language)

This is the most important part for **writing** charts. Helm templates use the **Go template language** plus a big bag of extra functions (from a library called Sprig).

Think of a template like a **Mad Libs** page:
> "Today I saw a ______ (animal) eating ______ (food)."

`{{ }}` marks each blank. Helm fills the blanks using your values.

> 💡 **Try every example yourself.** Put the template in a file in `templates/`, then run `helm template test ./your-chart`. Seeing the output is the fastest way to learn.

## 4.1 The built-in objects (where data comes from)

Inside `{{ }}`, the dot `.` means "everything I know right now." These are the main things inside it:

| Object | What it holds | Example |
|--------|--------------|---------|
| `.Values` | Your knobs from values.yaml, `-f`, and `--set` | `{{ .Values.replicaCount }}` |
| `.Release.Name` | The name you gave the install | `demo` |
| `.Release.Namespace` | Which namespace it's installed in | `default` |
| `.Release.IsInstall` / `.Release.IsUpgrade` | true/false | Do something only on first install |
| `.Release.Revision` | Revision number | `3` |
| `.Release.Service` | Always `Helm` | Used in labels |
| `.Chart.Name`, `.Chart.Version`, `.Chart.AppVersion` | Fields from Chart.yaml | `{{ .Chart.AppVersion }}` |
| `.Capabilities.KubeVersion.Version` | The cluster's Kubernetes version | `v1.34.1` |
| `.Capabilities.APIVersions.Has "x/v1"` | Does the cluster understand this kind? | Check if Istio is installed |
| `.Template.Name`, `.Template.BasePath` | Info about the current file | Used in the checksum trick |
| `.Files` | Read other (non-template) files in the chart | `{{ .Files.Get "config/app.conf" }}` |

**Example**

values.yaml:
```yaml
team: rockets
```
template:
```yaml
# Installed as {{ .Release.Name }} in {{ .Release.Namespace }} by team {{ .Values.team }}
```
output (`helm template demo ./chart`):
```yaml
# Installed as demo in default by team rockets
```

## 4.2 Pipelines `|` — passing data down an assembly line

A pipe `|` takes what's on the left and hands it to the function on the right, like a factory assembly line.

```yaml
name: {{ .Values.name | upper | quote }}
```
If `name: pizza` → output is `name: "PIZZA"`.
Step by step: `pizza` → `upper` → `PIZZA` → `quote` → `"PIZZA"`.

## 4.3 The functions you'll use most

| Function | What it does | Example | Result |
|----------|-------------|---------|--------|
| `quote` | Adds double quotes | `{{ "hi" \| quote }}` | `"hi"` |
| `squote` | Adds single quotes | `{{ "hi" \| squote }}` | `'hi'` |
| `upper` / `lower` | Change letter case | `{{ "Hi" \| upper }}` | `HI` |
| `default` | Use a backup value if empty | `{{ .Values.color \| default "blue" }}` | `blue` if color is empty |
| `required` | Stop with an error if missing | `{{ required "image.tag is required!" .Values.image.tag }}` | Error message if empty |
| `trunc 63` | Cut to 63 characters | names must be ≤ 63 chars | |
| `trimSuffix "-"` | Remove a dash at the end | `{{ "abc-" \| trimSuffix "-" }}` | `abc` |
| `replace` | Swap text | `{{ "a.b" \| replace "." "-" }}` | `a-b` |
| `printf` | Build a string | `{{ printf "%s-%s" "a" "b" }}` | `a-b` |
| `toYaml` | Turn a values block into YAML | `{{ toYaml .Values.resources }}` | |
| `nindent N` | New line, then indent N spaces | `{{ toYaml .Values.x \| nindent 4 }}` | |
| `indent N` | Indent N spaces (no new line) | | |
| `b64enc` | Base64-encode (for Secrets) | `{{ "pw" \| b64enc }}` | `cHc=` |
| `sha256sum` | Make a fingerprint of text | Checksum trick | |
| `randAlphaNum 16` | Random letters/numbers | Generate a password | |
| `include` | Use a named snippet and get it as text | `{{ include "app.labels" . }}` | |
| `tpl` | Treat a value as a template | `{{ tpl .Values.url . }}` | |
| `lookup` | Read something that already exists in the cluster | See Part 10 | |
| `fail` | Stop with a custom error | `{{ fail "bad setting" }}` | |
| `has` | Is an item in a list? | `{{ has "x" .Values.list }}` | true/false |
| `empty` | Is it empty? | `{{ if empty .Values.x }}` | |
| `eq`, `ne`, `lt`, `gt`, `and`, `or`, `not` | Comparisons and logic | `{{ if eq .Values.env "prod" }}` | |

## 4.4 Whitespace control: the `{{-` and `-}}` dashes

Template tags leave blank lines behind, and YAML cares a LOT about spaces. The dash is an **eraser**:
- `{{-` erases spaces and new lines **to the left**
- `-}}` erases spaces and new lines **to the right**

Without dashes:
```yaml
labels:
  {{ if .Values.extraLabel }}
  team: blue
  {{ end }}
```
Output (ugly blank lines):
```yaml
labels:

  team: blue

```

With dashes:
```yaml
labels:
  {{- if .Values.extraLabel }}
  team: blue
  {{- end }}
```
Output (clean):
```yaml
labels:
  team: blue
```

> ⚠️ Don't over-erase! `{{- ... -}}` on both sides can glue two lines together and break your YAML. When in doubt, use the left dash `{{-` only, and check with `helm template`.

## 4.5 `if` / `else` — making choices

values.yaml:
```yaml
environment: prod
debug: false
```
template:
```yaml
data:
  {{- if eq .Values.environment "prod" }}
  LOG_LEVEL: "warn"
  {{- else if eq .Values.environment "test" }}
  LOG_LEVEL: "info"
  {{- else }}
  LOG_LEVEL: "debug"
  {{- end }}
  {{- if not .Values.debug }}
  DEBUG_TOOLS: "off"
  {{- end }}
```
output:
```yaml
data:
  LOG_LEVEL: "warn"
  DEBUG_TOOLS: "off"
```

**Turning a whole file on or off** (very common!):
```yaml
{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
# ...rest of the file
{{- end }}
```
If `ingress.enabled` is false, Helm makes **nothing** from this file.

### What counts as "false"?
These are all treated as false in an `if`: `false`, `0`, `""` (empty text), `nil`/missing, empty list `[]`, empty map `{}`. Everything else is true.

## 4.6 `range` — doing something for each item (loops)

**Looping over a list**

values.yaml:
```yaml
fruits:
  - apple
  - banana
  - cherry
```
template:
```yaml
data:
  fruits.txt: |
    {{- range .Values.fruits }}
    - {{ . | title }}
    {{- end }}
```
output:
```yaml
data:
  fruits.txt: |
    - Apple
    - Banana
    - Cherry
```
Inside `range`, the dot `.` becomes **the current item**.

**Looping over a map (key + value)**

values.yaml:
```yaml
env:
  COLOR: blue
  SIZE: large
```
template:
```yaml
env:
  {{- range $key, $value := .Values.env }}
  - name: {{ $key }}
    value: {{ $value | quote }}
  {{- end }}
```
output (Helm sorts map keys alphabetically):
```yaml
env:
  - name: COLOR
    value: "blue"
  - name: SIZE
    value: "large"
```

**Looping with a counter**
```yaml
{{- range $i, $fruit := .Values.fruits }}
fruit-{{ $i }}: {{ $fruit }}
{{- end }}
```
→ `fruit-0: apple`, `fruit-1: banana`, `fruit-2: cherry`

## 4.7 `with` — zooming in

`with` changes the dot to a smaller part of the values, so you type less. It also skips the block if that part is empty.

values.yaml:
```yaml
database:
  host: db.example.com
  port: 5432
```
template:
```yaml
{{- with .Values.database }}
DB_HOST: {{ .host }}
DB_PORT: {{ .port | quote }}
{{- end }}
```
output:
```yaml
DB_HOST: db.example.com
DB_PORT: "5432"
```

## 4.8 Variables and the `$` sign

Inside `range` or `with`, the dot changes. So how do you reach `.Release.Name`? Use **`$`** — it always means "the top level," no matter where you are.

```yaml
{{- range .Values.fruits }}
- {{ $.Release.Name }}-{{ . }}
{{- end }}
```
→ `- demo-apple`, `- demo-banana`, `- demo-cherry`

You can also make your own variables:
```yaml
{{- $appName := printf "%s-web" .Release.Name }}
name: {{ $appName }}
```

## 4.9 Named templates: `define` and `include` (reusable snippets)

If you write the same labels in 6 files, and then need to change them, you'd have to edit 6 files. Instead, write it **once** in `_helpers.tpl`:

```yaml
{{/* templates/_helpers.tpl */}}

{{/* The short name of the app */}}
{{- define "hello-web.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* The full name: release name + app name */}}
{{- define "hello-web.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "hello-web.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/* Labels that go on EVERYTHING */}}
{{- define "hello-web.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{ include "hello-web.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/* Labels used to FIND pods (these must never change after install) */}}
{{- define "hello-web.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hello-web.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
```

Use them anywhere:
```yaml
metadata:
  name: {{ include "hello-web.fullname" . }}
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
```

**Why `include` and not `template`?** Both insert a snippet, but only `include` lets you pipe the result into `nindent`. Indentation matters in YAML, so **always use `include`**.

**Why the `.` at the end?** It passes "everything I know" into the snippet. Forget it, and the snippet can't see `.Release` or `.Values`.

> 📛 **Naming tip:** Start every snippet name with your chart name (`hello-web.labels`). Subcharts share one big snippet list, so two charts both defining `labels` would clash.

## 4.10 `toYaml` + `nindent` — copying whole blocks

Some settings are big blocks you just want to copy as-is.

values.yaml:
```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    memory: 256Mi
```
template:
```yaml
      containers:
        - name: web
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
```
output:
```yaml
      containers:
        - name: web
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              memory: 256Mi
```

**How do I pick the number for `nindent`?** Count the spaces before the line where the block should start. Here `requests:` must line up 12 spaces from the left, so `nindent 12`.

## 4.11 Comments

```yaml
{{/* This is a template comment. It disappears from the output. */}}
# This is a YAML comment. It stays in the output.
```
Use `{{/* */}}` for notes to other chart writers.

## 4.12 Reading extra files with `.Files`

Put a file in your chart (not in `templates/`), for example `files/nginx.conf`:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "hello-web.fullname" . }}-nginx
data:
  nginx.conf: |-
    {{- .Files.Get "files/nginx.conf" | nindent 4 }}
```
Or load a whole folder at once:
```yaml
data:
  {{- (.Files.Glob "files/*").AsConfig | nindent 2 }}
```

## 4.13 Data types gotcha (a classic bug!)

YAML guesses types. `true`, `yes`, `on` can become **booleans**, and `1.10` can become the number `1.1`. Kubernetes often wants **strings**.

```yaml
# values.yaml
image:
  tag: 1.10      # YAML turns this into the NUMBER 1.1 !!
```
Fix it two ways (do both):
```yaml
image:
  tag: "1.10"                                  # quote it in values
```
```yaml
image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"   # and quote in templates
value: {{ .Values.someSetting | quote }}
```

> 🔑 **Rule of thumb:** environment variable values, labels, and annotations should always be strings. Use `| quote`.

---

# Part 5 — Writing a Complete Chart, Piece by Piece

Now let's upgrade our `hello-web` chart into a "real" one that follows best practices. You can start from `helm create hello-web` and edit, or keep building your hand-made one.

## 5.1 The full `values.yaml`

```yaml
# -- How many copies of the app to run
replicaCount: 2

image:
  # -- Which picture (image) to run
  repository: nginx
  # -- Image version. Leave empty to use Chart.appVersion
  tag: ""
  pullPolicy: IfNotPresent

# -- Override the chart name in object names
nameOverride: ""
fullnameOverride: ""

# -- The message shown on the web page
message: "Hello from Helm!"

# -- Plain settings (become a ConfigMap)
config:
  LOG_LEVEL: info
  APP_MODE: production

# -- Secret settings. For learning ONLY. Use existingSecret in real life.
secret:
  create: false
  data: {}
  # -- Name of a Secret you already made outside Helm
  existingSecret: ""

serviceAccount:
  create: true
  name: ""

podAnnotations: {}

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 101
  fsGroup: 101

securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: false
  capabilities:
    drop: ["ALL"]

service:
  type: ClusterIP
  port: 80
  targetPort: 8080

# -- Gateway API route (the modern front door)
httpRoute:
  enabled: false
  gatewayName: main-gateway
  gatewayNamespace: default
  hostnames:
    - hello.example.com

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    memory: 128Mi

autoscaling:
  enabled: false
  minReplicas: 2
  maxReplicas: 5
  targetCPUUtilizationPercentage: 70

nodeSelector: {}
tolerations: []
affinity: {}
```

> 💡 The `# --` comments are a popular style. A tool called **helm-docs** reads them and writes your README table for you.

> 🔐 We switched the image to listen on port 8080 because the pod now runs as a **non-root user**, and non-root users can't use port 80. The `nginxinc/nginx-unprivileged` image does this. Change the image to:
> ```yaml
> image:
>   repository: nginxinc/nginx-unprivileged
> ```

## 5.2 The Deployment (with health checks and the checksum trick)

`templates/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "hello-web.fullname" . }}
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
spec:
  {{- if not .Values.autoscaling.enabled }}
  replicas: {{ .Values.replicaCount }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "hello-web.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        # ✨ The checksum trick: if the ConfigMap changes, this number changes,
        # so Kubernetes restarts the pods and they read the new settings.
        checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
        {{- with .Values.podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      labels:
        {{- include "hello-web.selectorLabels" . | nindent 8 }}
    spec:
      serviceAccountName: {{ include "hello-web.serviceAccountName" . }}
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
      containers:
        - name: web
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          securityContext:
            {{- toYaml .Values.securityContext | nindent 12 }}
          ports:
            - name: http
              containerPort: {{ .Values.service.targetPort }}
          # Is the app alive? If not, restart it.
          livenessProbe:
            httpGet:
              path: /
              port: http
          # Is the app ready for visitors? If not, don't send traffic yet.
          readinessProbe:
            httpGet:
              path: /
              port: http
          envFrom:
            - configMapRef:
                name: {{ include "hello-web.fullname" . }}-config
            {{- if or .Values.secret.create .Values.secret.existingSecret }}
            - secretRef:
                name: {{ .Values.secret.existingSecret | default (printf "%s-secret" (include "hello-web.fullname" .)) }}
            {{- end }}
          volumeMounts:
            - name: page
              mountPath: /usr/share/nginx/html
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
      volumes:
        - name: page
          configMap:
            name: {{ include "hello-web.fullname" . }}-config
            items:
              - key: index.html
                path: index.html
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
```

**Liveness vs. readiness, explained simply:**
- **Liveness** = "Are you awake?" If a student falls asleep (app frozen), the teacher wakes them up (restarts the container).
- **Readiness** = "Are you ready for questions?" A student still unpacking isn't called on yet (no traffic until ready).

## 5.3 The ConfigMap

`templates/configmap.yaml`:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "hello-web.fullname" . }}-config
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
data:
  {{- range $key, $value := .Values.config }}
  {{ $key }}: {{ $value | quote }}
  {{- end }}
  index.html: |
    <h1>{{ .Values.message }}</h1>
    <p>Chart {{ .Chart.Name }} v{{ .Chart.Version }} · release {{ .Release.Name }}</p>
```

## 5.4 The Secret (only if asked)

`templates/secret.yaml`:
```yaml
{{- if .Values.secret.create }}
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "hello-web.fullname" . }}-secret
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
type: Opaque
stringData:
  {{- range $key, $value := .Values.secret.data }}
  {{ $key }}: {{ $value | quote }}
  {{- end }}
{{- end }}
```

> 💡 `stringData` lets you write plain text and Kubernetes encodes it for you. With `data:` you must encode it yourself using `| b64enc`.
>
> ⚠️ **Base64 is NOT a lock.** It's like writing a note in pig latin — anyone can read it. Real protection comes from who's allowed to read Secrets (RBAC) and the tools in Part 10.

## 5.5 The Service

`templates/service.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "hello-web.fullname" . }}
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: http
      name: http
  selector:
    {{- include "hello-web.selectorLabels" . | nindent 4 }}
```

**Service types:**

| Type | Who can reach it | When to use |
|------|------------------|-------------|
| `ClusterIP` (default) | Only apps inside the cluster | Most apps — put a Gateway in front |
| `NodePort` | Anyone who can reach a node, on a high port (30000–32767) | Quick testing |
| `LoadBalancer` | The internet, through a cloud load balancer | Simple public apps on a cloud |

## 5.6 The ServiceAccount (the app's ID card)

Add to `_helpers.tpl`:
```yaml
{{- define "hello-web.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "hello-web.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
```

`templates/serviceaccount.yaml`:
```yaml
{{- if .Values.serviceAccount.create }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "hello-web.serviceAccountName" . }}
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
automountServiceAccountToken: false
{{- end }}
```

## 5.7 The front door: Gateway API `HTTPRoute`

The **Gateway API** is the modern replacement for Ingress ("Ingress 2.0"). It splits the front door into pieces so different people can own different parts:

- **GatewayClass** — "What kind of door?" (installed by whoever runs the cluster — e.g., Istio, Envoy Gateway, Cilium, NGINX Gateway Fabric, cloud providers)
- **Gateway** — "The actual door" (usually set up once by the cluster team)
- **HTTPRoute** — "Signs that point visitors to the right room" (this is the part **your app's chart** usually owns)

`templates/httproute.yaml`:
```yaml
{{- if .Values.httpRoute.enabled }}
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: {{ include "hello-web.fullname" . }}
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
spec:
  parentRefs:
    - name: {{ .Values.httpRoute.gatewayName }}
      namespace: {{ .Values.httpRoute.gatewayNamespace }}
  hostnames:
    {{- toYaml .Values.httpRoute.hostnames | nindent 4 }}
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: {{ include "hello-web.fullname" . }}
          port: {{ .Values.service.port }}
{{- end }}
```

**Gateway API vs. Ingress — pros and cons**

| | Gateway API (HTTPRoute) | Ingress |
|---|---|---|
| ✅ Pros | Modern, standard features (traffic splitting, header matching) without special annotations; works the same on many controllers; supported by Istio | Very simple; tons of old examples online |
| ❌ Cons | More objects to learn; your cluster needs a Gateway controller | Frozen (no new features); fancy features need controller-specific annotations; the popular **Ingress NGINX** controller was retired in March 2026 |
| 👉 Use when | Starting anything new | Maintaining an older cluster that already uses another supported Ingress controller |

> 💡 Many charts support **both**, with `ingress.enabled` and `httpRoute.enabled` switches. The `helm create` starter chart still includes an Ingress template.

## 5.8 The autoscaler (adds more copies when busy)

`templates/hpa.yaml`:
```yaml
{{- if .Values.autoscaling.enabled }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "hello-web.fullname" . }}
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ include "hello-web.fullname" . }}
  minReplicas: {{ .Values.autoscaling.minReplicas }}
  maxReplicas: {{ .Values.autoscaling.maxReplicas }}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .Values.autoscaling.targetCPUUtilizationPercentage }}
{{- end }}
```
Like a cafeteria opening more lunch lines when the line gets long. (Needs **metrics-server** in the cluster.)

> Notice the Deployment skips `replicas:` when autoscaling is on. Otherwise Helm and the autoscaler would fight over the number.

## 5.9 NOTES.txt (the "thank you" card)

`templates/NOTES.txt`:
```
🎉 {{ .Chart.Name }} is installed as "{{ .Release.Name }}" in namespace "{{ .Release.Namespace }}".

To visit it on your computer:
  kubectl port-forward -n {{ .Release.Namespace }} svc/{{ include "hello-web.fullname" . }} 8080:{{ .Values.service.port }}
  then open http://localhost:8080

{{- if .Values.httpRoute.enabled }}
It's also available at:
{{- range .Values.httpRoute.hostnames }}
  http://{{ . }}
{{- end }}
{{- end }}
```

## 5.10 Sidecars (helpers that ride along)

A **sidecar** is an extra container in the **same Pod** as your app. They share the same network and can share folders. Like a classroom aide who sits next to a student.

Common sidecars: log shippers, proxies (like Istio's Envoy), file syncers.

**Optional sidecar controlled by values:**

values.yaml:
```yaml
logSidecar:
  enabled: true
  image: busybox:1.37
```
in `deployment.yaml` under `containers:`:
```yaml
        {{- if .Values.logSidecar.enabled }}
        - name: log-helper
          image: {{ .Values.logSidecar.image }}
          command: ["sh", "-c", "while true; do date >> /logs/heartbeat.log; sleep 30; done"]
          volumeMounts:
            - name: shared-logs
              mountPath: /logs
        {{- end }}
```
and under `volumes:`:
```yaml
        {{- if .Values.logSidecar.enabled }}
        - name: shared-logs
          emptyDir: {}
        {{- end }}
```

> 💡 Newer Kubernetes versions also support **native sidecars**: put the helper in `initContainers` with `restartPolicy: Always`. It starts before your app and stops after it — handy for proxies.

## 5.11 NetworkPolicy (who's allowed to talk to whom)

Like a rule that says "only students from the Science class can enter the Lab."

`templates/networkpolicy.yaml`:
```yaml
{{- if .Values.networkPolicy.enabled }}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "hello-web.fullname" . }}
spec:
  podSelector:
    matchLabels:
      {{- include "hello-web.selectorLabels" . | nindent 6 }}
  policyTypes: ["Ingress"]
  ingress:
    - from:
        {{- toYaml .Values.networkPolicy.allowFrom | nindent 8 }}
      ports:
        - port: {{ .Values.service.targetPort }}
{{- end }}
```
values.yaml:
```yaml
networkPolicy:
  enabled: false
  allowFrom:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: gateway-system
```
(NetworkPolicies only work if your cluster's network plugin supports them, like Calico or Cilium.)

## 5.12 How Helm ties everything together

```
values.yaml  ─┐
-f my.yaml   ─┼──►  Helm fills every {{ }}  ──►  finished YAML  ──►  Kubernetes
--set x=y    ─┘          in templates/                               creates/updates
                                                                     everything
```

1. Helm gathers all values (defaults + your overrides).
2. It renders every template.
3. It sends the result to Kubernetes.
4. It saves a record of this revision (as a Secret in the namespace) so it can roll back later.

---

# Part 6 — Values: Turning the Knobs

## 6.1 Four ways to give values

```bash
# 1. Use the defaults in values.yaml
helm install demo ./hello-web

# 2. A values file (best for real use — you can save it in Git)
helm install demo ./hello-web -f values-prod.yaml

# 3. --set on the command line (good for quick tests)
helm install demo ./hello-web --set replicaCount=3

# 4. --set-string / --set-file / --set-json for special cases
helm install demo ./hello-web --set-string image.tag=1.10
helm install demo ./hello-web --set-file config.extra=./extra.conf
helm install demo ./hello-web --set-json 'resources={"limits":{"memory":"512Mi"}}'
```

## 6.2 Who wins? (The order of importance)

From **weakest** to **strongest**:
1. The chart's own `values.yaml`
2. A parent chart's values (for subcharts)
3. `-f` files, in order — **the last one wins**
4. `--set` flags — **strongest of all**

**Example:**
```bash
helm install demo ./hello-web -f values-dev.yaml -f values-mine.yaml --set replicaCount=4
```
If `values.yaml` says 2, `values-dev.yaml` says 1, `values-mine.yaml` says 3, and `--set` says 4 → you get **4**.

## 6.3 `--set` cheat sheet

| You type | You get |
|----------|---------|
| `--set a=b` | `a: b` |
| `--set a.b.c=1` | nested: `a: {b: {c: 1}}` |
| `--set list={x,y,z}` | `list: [x, y, z]` |
| `--set servers[0].port=80` | first item of a list |
| `--set name=a\,b` | text with a comma: `a,b` |
| `--set "nodeSelector.kubernetes\.io/os=linux"` | a key with a dot in it |
| `--set a=null` | deletes a default |

## 6.4 One chart, many environments

```
hello-web/
values-dev.yaml
values-staging.yaml
values-prod.yaml
```

`values-dev.yaml`:
```yaml
replicaCount: 1
message: "Hello from DEV 🧪"
config:
  LOG_LEVEL: debug
resources:
  requests: { cpu: 10m, memory: 32Mi }
```

`values-prod.yaml`:
```yaml
message: "Welcome!"
autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
httpRoute:
  enabled: true
  hostnames: [hello.mycompany.com]
secret:
  existingSecret: hello-web-prod-secrets
```

```bash
helm upgrade --install hello-dev  ./hello-web -n dev  --create-namespace -f values-dev.yaml
helm upgrade --install hello-prod ./hello-web -n prod --create-namespace -f values-prod.yaml
```

> 💡 `helm upgrade --install` means "install it if it isn't there, upgrade it if it is." Great for scripts and CI pipelines.

## 6.5 Values schema — catching mistakes early

A `values.schema.json` file is like a **spell-checker for your values**. Helm checks it during `install`, `upgrade`, `lint`, and `template`.

`hello-web/values.schema.json`:
```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["image", "service"],
  "properties": {
    "replicaCount": {
      "type": "integer",
      "minimum": 1,
      "maximum": 20
    },
    "message": {
      "type": "string",
      "maxLength": 200
    },
    "image": {
      "type": "object",
      "required": ["repository"],
      "properties": {
        "repository": { "type": "string" },
        "tag": { "type": "string" },
        "pullPolicy": { "enum": ["Always", "IfNotPresent", "Never"] }
      }
    },
    "service": {
      "type": "object",
      "properties": {
        "type": { "enum": ["ClusterIP", "NodePort", "LoadBalancer"] },
        "port": { "type": "integer" }
      }
    }
  }
}
```

Now try a mistake:
```bash
helm template demo ./hello-web --set replicaCount=banana
# Error: values don't meet the specifications of the schema(s)...
#   replicaCount: Invalid type. Expected: integer, given: string
```
Caught before it ever reached the cluster! ✅

## 6.6 See what values a release is using

```bash
helm get values demo           # only the values YOU changed
helm get values demo --all     # every value, including defaults
helm show values ./hello-web   # the chart's default values
```

---

# Part 7 — Testing and Debugging

Like proofreading an essay before you turn it in. **Always** test before installing into a real cluster.

## 7.1 The testing ladder (easy → thorough)

| Step | Command | What it checks | Needs a cluster? |
|------|---------|----------------|------------------|
| 1 | `helm lint ./hello-web` | Chart structure, obvious errors, schema | No |
| 2 | `helm template demo ./hello-web` | Shows finished YAML | No |
| 3 | `helm template demo ./hello-web --debug` | Same, plus more detail when broken | No |
| 4 | `helm install demo ./hello-web --dry-run=server` | Asks the real cluster "would you accept this?" | Yes |
| 5 | `helm install demo ./hello-web --wait` | Installs and waits until pods are ready | Yes |
| 6 | `helm test demo` | Runs your test pods | Yes |

## 7.2 Render just one file

```bash
helm template demo ./hello-web -s templates/deployment.yaml
```

## 7.3 Writing a `helm test`

A test is a Pod with a special annotation. If it finishes successfully, the test passes.

`templates/tests/test-connection.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: {{ include "hello-web.fullname" . }}-test
  labels:
    {{- include "hello-web.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": test
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  restartPolicy: Never
  containers:
    - name: check-message
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          wget -qO- http://{{ include "hello-web.fullname" . }}:{{ .Values.service.port }} \
            | grep -q {{ .Values.message | quote }}
```
```bash
helm test demo
# Phase: Succeeded ✅
```

## 7.4 Looking at what's installed

```bash
helm list -A                    # every release in every namespace
helm status demo                # summary + NOTES.txt again
helm history demo               # every revision
helm get manifest demo          # the exact YAML Helm sent to Kubernetes
helm get notes demo
kubectl describe pod <pod-name> # why is my pod unhappy?
kubectl logs <pod-name>         # what is my app saying?
```

## 7.5 Helpful extra tools

| Tool | What it does |
|------|-------------|
| **helm-diff** plugin | Shows exactly what an upgrade would change, like "track changes" |
| **helm-unittest** plugin | Write small tests for your templates (no cluster needed) |
| **chart-testing (`ct`)** | Lints and test-installs charts in CI pipelines |
| **kubeconform** | Checks rendered YAML against Kubernetes rules |
| **helm-docs** | Builds a README table from your values.yaml comments |

> 💡 Helm 4 changed how plugins work. Check each plugin's README for Helm 4 install steps.

**Example: a helm-unittest test** (`hello-web/tests/deployment_test.yaml`):
```yaml
suite: deployment tests
templates:
  - templates/deployment.yaml
tests:
  - it: uses the replica count from values
    set:
      replicaCount: 3
    asserts:
      - equal:
          path: spec.replicas
          value: 3
  - it: has no replicas field when autoscaling is on
    set:
      autoscaling.enabled: true
    asserts:
      - notExists:
          path: spec.replicas
```

## 7.6 Reading error messages (decoder ring)

| Error says... | It usually means... | Fix |
|---------------|--------------------|-----|
| `error converting YAML to JSON: yaml: line 12: did not find expected key` | Bad indentation | Check `nindent` numbers; run `helm template --debug` |
| `nil pointer evaluating interface {}.port` | You used `.Values.service.port` but `service` is missing | Add it to values.yaml or use `with`/`default` |
| `cannot re-use a name that is still in use` | A release with that name already exists | Use `helm upgrade`, or pick a new name |
| `another operation (install/upgrade/rollback) is in progress` | A previous command got stuck | Wait, or `helm rollback` to the last good revision |
| `field is immutable` (on selector) | You changed selector labels after install | Never change `selectorLabels`; reinstall if you must |
| `values don't meet the specifications of the schema` | Your schema caught a bad value | Read which field and fix it |

---

# Part 8 — Adding Login with Keycloak

## 8.1 What is Keycloak?

**Keycloak** is like the school's **ID-badge office**. Before you can enter certain rooms (apps), you go to the office, prove who you are (log in), and get a **badge** (a token). Apps check the badge instead of asking for your password themselves.

Key words:
| Word | School version |
|------|---------------|
| **Realm** | A whole school district with its own students and rules |
| **Client** | An app that wants to check badges (your web app) |
| **User** | A student or teacher |
| **Role** | "Student," "Teacher," "Principal" — what you're allowed to do |
| **Token (JWT)** | The badge — a signed card saying who you are and what roles you have |
| **OIDC** | The standard language apps and Keycloak use to talk about badges |

## 8.2 Installing Keycloak with Helm

The community **codecentric `keycloakx`** chart is a popular choice. (It replaced their older `keycloak` chart.)

**Step 1 — Make the admin password a Secret (don't type it into values!):**
```bash
kubectl create namespace auth
kubectl create secret generic keycloak-admin -n auth \
  --from-literal=KC_BOOTSTRAP_ADMIN_USERNAME=admin \
  --from-literal=KC_BOOTSTRAP_ADMIN_PASSWORD='Pick-A-Strong-One-123!'
```

**Step 2 — Write `keycloak-values.yaml` (learning setup):**
```yaml
# Learning mode: "start-dev" uses a built-in practice database.
# Don't use start-dev for real users — see the production notes below.
command:
  - "/opt/keycloak/bin/kc.sh"
  - "start-dev"

# Serve at "/" instead of the chart's default "/auth"
http:
  relativePath: "/"

# Load the admin username/password from our Secret
extraEnvFrom: |
  - secretRef:
      name: keycloak-admin
```

**Step 3 — Install:**
```bash
helm repo add codecentric https://codecentric.github.io/helm-charts
helm repo update
helm install keycloak codecentric/keycloakx -n auth -f keycloak-values.yaml
kubectl get pods -n auth -w      # wait for Running and READY 1/1 (can take a minute or two)
```

**Step 4 — Open the admin console:**
```bash
kubectl port-forward -n auth svc/keycloak-keycloakx-http 8080:80
```
Visit http://localhost:8080 and log in with the username and password from your Secret.

> 💡 Newer Keycloak versions use `KC_BOOTSTRAP_ADMIN_USERNAME` / `KC_BOOTSTRAP_ADMIN_PASSWORD`. Older guides show `KEYCLOAK_ADMIN` or `KEYCLOAK_USER`. If a guide doesn't work, check which Keycloak version it was written for.

**Step 5 — Set up a realm and client (in the admin console):**
1. Create a realm called `myrealm`.
2. Go to **Clients → Create client**, name it `hello-web`, protocol **OpenID Connect**.
3. Turn on **Client authentication** if your app has a server side (it gets a client secret).
4. Add **Valid redirect URIs**, like `http://localhost:8081/*`.
5. Create a user under **Users**, then set a password under **Credentials**.

**Production notes (when you're ready for real users):**
- Use `start` (not `start-dev`) and a real database (PostgreSQL). The keycloakx chart has a `database:` section for this.
- Turn on HTTPS and set the public hostname (`KC_HOSTNAME`).
- Run at least 2 replicas.
- Many teams use the official **Keycloak Operator** instead of a Helm chart.

| Option | ✅ Pros | ❌ Cons |
|--------|--------|--------|
| codecentric `keycloakx` Helm chart | Simple Helm workflow, many settings | Community-run; you manage upgrades |
| Keycloak Operator (official) | Made by the Keycloak team; manages upgrades for you | One more thing (an operator) to install and learn |
| Hosted login service (cloud) | No servers to run | Costs money; less control |

## 8.3 Connecting your app to Keycloak with your chart

Add a section to your app's `values.yaml`:
```yaml
auth:
  enabled: true
  issuerUrl: "http://keycloak-keycloakx-http.auth.svc.cluster.local/realms/myrealm"
  clientId: hello-web
  # The client secret lives in a Secret made outside Helm
  clientSecretName: hello-web-oidc
```

In `templates/deployment.yaml`, under the container:
```yaml
          {{- if .Values.auth.enabled }}
          env:
            - name: OIDC_ISSUER_URL
              value: {{ .Values.auth.issuerUrl | quote }}
            - name: OIDC_CLIENT_ID
              value: {{ .Values.auth.clientId | quote }}
            - name: OIDC_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.auth.clientSecretName }}
                  key: client-secret
          {{- end }}
```

Make the client secret (copy it from Keycloak → Clients → hello-web → Credentials):
```bash
kubectl create secret generic hello-web-oidc --from-literal=client-secret='PASTE-IT-HERE'
```

**Two ways to protect an app:**

| Way | How it works | ✅ Pros | ❌ Cons |
|-----|-------------|--------|--------|
| **App does it** | Your code uses an OIDC library (Spring Security, Passport.js, Authlib...) | Full control; app knows the user | You must write code |
| **Something in front does it** | oauth2-proxy, or Istio checks tokens (next part) | No app code changes | Less detail inside the app |

> 🔤 **What does the issuer URL look like?** `http://<keycloak-service>.<namespace>.svc.cluster.local/realms/<realm-name>`. Every Keycloak realm also has a "discovery" page at `<issuer>/.well-known/openid-configuration` that lists all its other addresses.

---

# Part 9 — Traffic Control and Security with Istio

## 9.1 What is Istio?

**Istio** is a **service mesh** — like adding traffic cops and security guards to every hallway in the school.

It can:
- 🔒 **Encrypt** all traffic between apps automatically (called **mTLS** — both sides show ID)
- 🚦 **Split traffic**: send 90% of visitors to version 1 and 10% to version 2 (a **canary** release)
- 🔁 Add **retries, timeouts, and circuit breakers** so one broken app doesn't crash everything
- 🎫 **Check login badges** (JWT tokens from Keycloak) before requests reach your app
- 📊 Collect **metrics** about who talks to whom

## 9.2 Sidecar mode vs. ambient mode

Istio has two ways to work:

**Sidecar mode** — every Pod gets its own personal traffic cop (an **Envoy** proxy container).
```bash
kubectl label namespace default istio-injection=enabled
```

**Ambient mode** (production-ready since Istio 1.24) — instead of one cop per classroom, there's a shared cop **per building** (called **ztunnel**) for encryption, plus optional **waypoint** proxies when you need fancy traffic rules.
```bash
kubectl label namespace default istio.io/dataplane-mode=ambient
```

| | Sidecar mode | Ambient mode |
|---|---|---|
| ✅ Pros | Very mature; every feature per pod | Uses much less memory/CPU; no pod restarts to join; simpler upgrades |
| ❌ Cons | Extra container in every pod; pods must restart to join | L7 features (routing, JWT checks) need a **waypoint** proxy set up |
| 👉 Good for | Teams already using it | New setups that want mTLS without the overhead |

**Controlling sidecars from your chart:**
```yaml
# values.yaml
podAnnotations:
  sidecar.istio.io/inject: "true"
```

## 9.3 Installing Istio with Helm

Istio itself is installed with Helm charts (the order matters):
```bash
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update

kubectl create namespace istio-system
helm install istio-base istio/base -n istio-system          # 1. the CRDs (new object types)
helm install istiod istio/istiod -n istio-system --wait     # 2. the control center
```
For ambient mode, you also install `istio/cni` and `istio/ztunnel`, and pass `--set profile=ambient`. Follow the official install guide for your Istio version — the steps change a little between versions.

## 9.4 Canary releases — two ways

To canary, you need **two Deployments** (v1 and v2) with a `version` label each.

### Way A: Istio `VirtualService` + `DestinationRule`

`templates/istio-destinationrule.yaml`:
```yaml
{{- if .Values.istio.enabled }}
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: {{ include "hello-web.fullname" . }}
spec:
  host: {{ include "hello-web.fullname" . }}
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
{{- end }}
```

`templates/istio-virtualservice.yaml`:
```yaml
{{- if .Values.istio.enabled }}
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: {{ include "hello-web.fullname" . }}
spec:
  hosts:
    - {{ include "hello-web.fullname" . }}
  http:
    - route:
        - destination:
            host: {{ include "hello-web.fullname" . }}
            subset: v1
          weight: {{ .Values.istio.canary.stableWeight }}
        - destination:
            host: {{ include "hello-web.fullname" . }}
            subset: v2
          weight: {{ sub 100 .Values.istio.canary.stableWeight }}
      timeout: 5s
      retries:
        attempts: 3
        perTryTimeout: 2s
{{- end }}
```

values.yaml:
```yaml
istio:
  enabled: false
  canary:
    stableWeight: 90     # v2 automatically gets 100 - 90 = 10
```

Move more traffic to v2, little by little:
```bash
helm upgrade demo ./hello-web --set istio.enabled=true --set istio.canary.stableWeight=50
helm upgrade demo ./hello-web --set istio.enabled=true --set istio.canary.stableWeight=0
```

> ✨ Notice `{{ sub 100 .Values.istio.canary.stableWeight }}`. The template does the math so the weights **always add up to 100**. One knob instead of two = fewer mistakes.

> 📌 Use `networking.istio.io/v1`. Older tutorials show `v1beta1` or `v1alpha3`; those still exist but `v1` is the stable version.

### Way B: Gateway API `HTTPRoute` with weights

Make two Services (`hello-web-v1`, `hello-web-v2`), then:
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: hello-web-canary
spec:
  parentRefs:
    - name: main-gateway
  rules:
    - backendRefs:
        - name: hello-web-v1
          port: 80
          weight: 90
        - name: hello-web-v2
          port: 80
          weight: 10
```

| | Istio VirtualService | Gateway API HTTPRoute |
|---|---|---|
| ✅ Pros | Lots of features (fault injection, mirroring) | Standard — works with Istio **and** other gateways |
| ❌ Cons | Only works with Istio | Fewer advanced features |

## 9.5 Turning on strict encryption (mTLS)

```yaml
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: {{ .Release.Namespace }}
spec:
  mtls:
    mode: STRICT      # only accept encrypted traffic from apps in the mesh
```

## 9.6 Putting it together: Istio checks Keycloak badges 🎫

This is where Keycloak and Istio team up. Istio can check the token **before** the request reaches your app.

`templates/istio-auth.yaml`:
```yaml
{{- if and .Values.istio.enabled .Values.auth.enabled }}
# 1. "Here's how to check a badge from Keycloak"
apiVersion: security.istio.io/v1
kind: RequestAuthentication
metadata:
  name: {{ include "hello-web.fullname" . }}-jwt
spec:
  selector:
    matchLabels:
      {{- include "hello-web.selectorLabels" . | nindent 6 }}
  jwtRules:
    - issuer: {{ .Values.auth.publicIssuerUrl | quote }}
      jwksUri: {{ printf "%s/protocol/openid-connect/certs" .Values.auth.issuerUrl | quote }}
---
# 2. "No valid badge? No entry."
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: {{ include "hello-web.fullname" . }}-require-jwt
spec:
  selector:
    matchLabels:
      {{- include "hello-web.selectorLabels" . | nindent 6 }}
  action: ALLOW
  rules:
    - from:
        - source:
            requestPrincipals: ["*"]   # any valid token
{{- end }}
```

values.yaml additions:
```yaml
auth:
  enabled: true
  # Where Istio downloads Keycloak's public keys (inside the cluster)
  issuerUrl: "http://keycloak-keycloakx-http.auth.svc.cluster.local/realms/myrealm"
  # The "iss" field inside tokens — the address users used to log in
  publicIssuerUrl: "https://login.mycompany.com/realms/myrealm"
```

> ⚠️ **The #1 gotcha:** the `issuer` must **exactly** match the `iss` field inside the token, and that depends on the web address the user used to log in. Paste a token into a JWT decoder (only test tokens!) to see the `iss` value.
>
> 💡 In **ambient mode**, `RequestAuthentication` and token rules need a **waypoint** proxy and use `targetRefs` instead of `selector`. Check the Istio docs for your version.

**Test it:**
```bash
# No token → 403 Forbidden ("RBAC: access denied")
curl -i http://hello.example.com/

# With a token → 200 OK
TOKEN=$(curl -s -X POST "$PUBLIC_ISSUER/protocol/openid-connect/token" \
  -d grant_type=client_credentials -d client_id=hello-web -d client_secret="$SECRET" \
  | jq -r .access_token)
curl -i -H "Authorization: Bearer $TOKEN" http://hello.example.com/
```

## 9.7 Only create Istio objects if Istio is installed

Use `.Capabilities` so your chart doesn't break in clusters without Istio:
```yaml
{{- if and .Values.istio.enabled (.Capabilities.APIVersions.Has "networking.istio.io/v1") }}
# ...Istio objects...
{{- end }}
```
> Note: `helm template` (without a cluster) doesn't know what your cluster has. Use `--api-versions networking.istio.io/v1` to pretend.

---

# Part 10 — Advanced Examples

## 10.1 Dependencies (subcharts) — charts inside charts

Like a recipe that says "first, make the pie crust using Grandma's crust recipe."

`Chart.yaml`:
```yaml
dependencies:
  - name: keycloakx
    version: "7.x.x"                      # any 7.something version
    repository: "https://codecentric.github.io/helm-charts"
    condition: keycloakx.enabled          # only include if this value is true
  - name: postgresql
    version: "~16.0.0"
    repository: "oci://registry.example.com/charts"
    alias: db                             # call it "db" in values
    condition: db.enabled
```

```bash
helm dependency update ./hello-web   # downloads into charts/ and writes Chart.lock
helm dependency list ./hello-web
```

**Passing values to a subchart:** put them under the subchart's name (or alias):
```yaml
keycloakx:
  enabled: true
  command: ["/opt/keycloak/bin/kc.sh", "start-dev"]

db:
  enabled: false
```

**Sharing values with ALL subcharts:** use `global`:
```yaml
global:
  imageRegistry: registry.mycompany.com
```
Every chart can read `.Values.global.imageRegistry`.

> 💡 **Commit `Chart.lock`** to Git so everyone gets the same subchart versions.

> 📦 **Note on Bitnami charts:** many older tutorials use Bitnami charts (like `bitnami/postgresql`). In 2025 Bitnami changed how its free images and charts are offered, so check the current terms or pick another source (an official operator like CloudNativePG for PostgreSQL is a popular choice).

## 10.2 Umbrella charts (one chart to install a whole system)

An **umbrella chart** has few or no templates of its own — it just lists subcharts. Like a "Back to School" bundle with a backpack, binder, and pencils.

```
school-platform/
├── Chart.yaml       # dependencies: frontend, api, keycloakx, db
└── values.yaml      # settings for each piece
```

| ✅ Pros | ❌ Cons |
|--------|--------|
| One command installs everything | One big release — upgrading one piece means upgrading the release |
| Shared settings with `global` | Harder to update pieces on different schedules |

## 10.3 Library charts (shared helper snippets)

A **library chart** has only snippets (no Kubernetes objects). Teams use it so all their charts share the same labels and patterns.

`common-lib/Chart.yaml`:
```yaml
apiVersion: v2
name: common-lib
type: library
version: 1.0.0
```
`common-lib/templates/_labels.tpl`:
```yaml
{{- define "common-lib.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
team: {{ .Values.team | default "unknown" }}
{{- end }}
```
Use it in any app chart (add `common-lib` as a dependency first):
```yaml
metadata:
  labels:
    {{- include "common-lib.labels" . | nindent 4 }}
```

## 10.4 Hooks — run something at special moments

Hooks are like school bells: "before class starts," "after class ends."

| Hook | When it runs |
|------|-------------|
| `pre-install` | Before anything is installed |
| `post-install` | After everything is installed |
| `pre-upgrade` / `post-upgrade` | Before/after an upgrade |
| `pre-rollback` / `post-rollback` | Before/after a rollback |
| `pre-delete` / `post-delete` | Before/after uninstall |
| `test` | When you run `helm test` |

**Example: database migration before each upgrade**
```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "hello-web.fullname" . }}-migrate
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "-5"                 # lower numbers run first
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  backoffLimit: 2
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: migrate
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          command: ["./migrate", "--up"]
```

| ✅ Pros | ❌ Cons |
|--------|--------|
| Easy "do this first" steps | Hook objects aren't managed like normal objects (Helm doesn't upgrade or delete them unless you set a delete policy) |
| Upgrade stops if the hook fails | GitOps tools treat hooks a bit differently |

## 10.5 Generating a password once (and keeping it) with `lookup`

Problem: `randAlphaNum 16` makes a **new** password every upgrade, which breaks things. Solution: check if the Secret already exists.

```yaml
{{- $secretName := printf "%s-db" (include "hello-web.fullname" .) }}
{{- $existing := lookup "v1" "Secret" .Release.Namespace $secretName }}
apiVersion: v1
kind: Secret
metadata:
  name: {{ $secretName }}
  annotations:
    "helm.sh/resource-policy": keep        # don't delete on uninstall
type: Opaque
data:
  {{- if $existing }}
  password: {{ index $existing.data "password" }}
  {{- else }}
  password: {{ randAlphaNum 24 | b64enc }}
  {{- end }}
```
> ⚠️ `lookup` returns nothing during `helm template` and `--dry-run=client` (there's no cluster to look at). Some GitOps tools don't support it either.

## 10.6 Keeping secrets safe — your options

**Never put real passwords in values files stored in Git.** Here are the grown-up options:

| Option | How it works | ✅ Pros | ❌ Cons |
|--------|-------------|--------|--------|
| **`existingSecret` value** | Someone creates the Secret separately; chart just uses its name | Simplest; chart has no secrets | Someone must create it by hand/scripts |
| **External Secrets Operator** | Chart makes an `ExternalSecret`; operator copies from a vault (AWS, Azure, GCP, HashiCorp Vault) | Secrets live in a real vault; auto-rotation | Must install and set up the operator + vault |
| **Sealed Secrets** | You encrypt secrets with a public key; only the cluster can decrypt | Safe to commit to Git | Tied to one cluster's key; must back up that key |
| **SOPS + helm-secrets plugin** | Encrypted values files, decrypted at install time | Works with normal Helm workflow | Everyone needs decryption keys set up |

**Example: External Secrets template in your chart**
```yaml
{{- if .Values.externalSecret.enabled }}
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: {{ include "hello-web.fullname" . }}
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: {{ .Values.externalSecret.storeName }}
  target:
    name: {{ include "hello-web.fullname" . }}-secret
  data:
    {{- range .Values.externalSecret.keys }}
    - secretKey: {{ .name }}
      remoteRef:
        key: {{ .remoteKey }}
    {{- end }}
{{- end }}
```

## 10.7 Packaging and sharing charts (OCI registries)

The modern way to share charts is an **OCI registry** — the same kind of place that stores container images (GitHub Container Registry, Docker Hub, Harbor, AWS ECR, Azure ACR, Google Artifact Registry).

```bash
helm package ./hello-web                                  # makes hello-web-0.1.0.tgz
helm registry login ghcr.io -u my-username                # log in
helm push hello-web-0.1.0.tgz oci://ghcr.io/my-username/charts

# Anyone can now install it:
helm install demo oci://ghcr.io/my-username/charts/hello-web --version 0.1.0
helm show values oci://ghcr.io/my-username/charts/hello-web --version 0.1.0
```

| | OCI registry | Classic HTTP chart repo (`index.yaml`) |
|---|---|---|
| ✅ Pros | Same place as your images; good security tools; no `helm repo add` needed | Very common; easy to browse |
| ❌ Cons | Can't list all charts with `helm search repo` | Needs a web server and an index file |

**Sign your charts** so others can check they weren't changed: `helm package --sign`, or use **cosign** for OCI charts.

## 10.8 GitOps — letting a robot run Helm for you

With **GitOps**, you store your chart and values in Git, and a tool in the cluster keeps things matching Git automatically. Like a hall monitor who checks every room matches the official seating chart.

- **Argo CD** — has a nice web dashboard; renders Helm charts and applies them
- **Flux** — uses a `HelmRelease` object; very "Kubernetes-native"

**Flux example:**
```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: hello-web
  namespace: prod
spec:
  interval: 10m
  chart:
    spec:
      chart: hello-web
      version: "0.1.x"
      sourceRef:
        kind: HelmRepository
        name: my-charts
  values:
    replicaCount: 3
    message: "Deployed by Flux!"
```

| ✅ Pros of GitOps | ❌ Cons |
|-------------------|--------|
| Every change is reviewed in Git; easy to see history | Another system to learn and run |
| Cluster fixes itself if someone changes things by hand | Debugging has one more layer |

## 10.9 Helm 4 features worth knowing

- **Server-side apply** (`--server-side`) — Kubernetes itself merges changes, which avoids fights when several tools edit the same object.
- **New plugin system** — plugins can be WebAssembly, so they work on any computer. Post-renderers are now plugins.
- **Reproducible chart packages** — packaging the same chart twice gives the exact same file.
- **Structured logging** — easier to plug into log tools.
- A future **chart `apiVersion: v3`** is being designed. For now, keep using `v2`.

---

# Part 11 — Pros and Cons: Helm vs. Other Tools

| Tool | How it works | ✅ Pros | ❌ Cons | Best for |
|------|-------------|--------|--------|----------|
| **Plain YAML** (`kubectl apply`) | Write every file yourself | Nothing to learn; what you see is what you get | Lots of copying; no versions or rollback | Tiny projects, learning |
| **Helm** | Templates + values | Huge library of ready charts; versions; rollback; hooks; sharing | Template syntax can get messy; YAML whitespace bugs | Apps you share or install many times |
| **Kustomize** (built into kubectl) | Start with base YAML, apply "patches" | No template language; plain YAML | No packaging or rollback; complex variations get awkward | Your own apps with a few environments |
| **Helm + Kustomize** | Render with Helm, tweak with Kustomize | Use public charts but patch what they don't expose | Two tools to understand | Adjusting third-party charts |
| **Operators** | A program in the cluster manages the app | Can handle hard jobs (backups, failover) | Someone has to write and maintain the operator | Databases, Keycloak, complex systems |

**Helm overall:**

| ✅ Pros | ❌ Cons |
|--------|--------|
| One command to install, upgrade, roll back | Templates can become hard to read |
| Thousands of ready-made charts on Artifact Hub | Easy to make indentation mistakes |
| Same chart, different values for each environment | CRDs (new object types) have special upgrade rules |
| Release history built in | Hook objects need care |

---

# Part 12 — Best Practices Checklist

**Chart structure**
1. ✅ Start with `helm create` or a team template — good bones matter.
2. ✅ Bump `version` in Chart.yaml **every** time the chart changes.
3. ✅ Use the standard labels (`app.kubernetes.io/name`, `/instance`, `/version`, `/managed-by`).
4. ✅ Never change selector labels after the first release.
5. ✅ Prefix snippet names with your chart name (`hello-web.labels`).
6. ✅ Use `include "chart.fullname"` for names so two releases can live side-by-side.

**Values**

7. ✅ Put everything that might change in `values.yaml`, with good defaults that work out of the box.
8. ✅ Comment every value (`# --`) and generate the README with helm-docs.
9. ✅ Use camelCase names (`replicaCount`, not `replica_count` or `ReplicaCount`).
10. ✅ Add a `values.schema.json`.
11. ✅ Quote strings (`| quote`) — especially version numbers.
12. ✅ Prefer maps over lists for things people need to override (maps merge, lists get replaced completely).

**Security**

13. ✅ Never commit real secrets. Support `existingSecret` or External Secrets.
14. ✅ Run as non-root, drop capabilities, set `allowPrivilegeEscalation: false`.
15. ✅ Pin image tags (or digests). Avoid `latest`.
16. ✅ Set resource requests (and memory limits).
17. ✅ Give the app its own ServiceAccount; don't auto-mount tokens it doesn't need.

**Reliability**

18. ✅ Add liveness and readiness probes.
19. ✅ Use the checksum annotation so config changes restart pods.
20. ✅ Use `helm upgrade --install` in pipelines, with `--atomic` (Helm 3) / rollback-on-failure (Helm 4: `--rollback-on-failure`) so failed upgrades undo themselves.

**Testing**

21. ✅ Run `helm lint` and `helm template` every time.
22. ✅ Write `helm test` pods and unit tests.
23. ✅ Use `--dry-run=server` before touching production.

---

# Part 13 — Common Mistakes (and Fixes)

| ❌ Mistake | 😬 What happens | ✅ Fix |
|-----------|----------------|-------|
| Wrong `nindent` number | YAML error or settings in the wrong place | Count the spaces; check with `helm template` |
| Forgetting the `.` in `include "x" .` | Snippet can't see values; empty output | Always pass `.` (or `$` inside loops) |
| Using `.Release.Name` inside `range` | Error: can't find Release | Use `$.Release.Name` |
| `tag: 1.10` without quotes | Becomes `1.1` | `tag: "1.10"` and use `| quote` |
| Changing selector labels | Upgrade fails: "field is immutable" | Keep selectorLabels the same forever |
| Editing pods/objects with `kubectl edit` | Next `helm upgrade` overwrites your change | Change values, then `helm upgrade` |
| `randAlphaNum` password in a Secret | New password every upgrade → app breaks | Use `lookup` or `existingSecret` |
| Forgetting to bump the chart version | People can't tell versions apart | Bump `version` every change |
| Not running `helm dependency update` | Error: missing subchart | Run it after editing dependencies |
| Setting `replicas` with an autoscaler on | Helm and autoscaler fight | Skip `replicas` when autoscaling is enabled |
| Putting secrets in `--set` on the command line | They end up in shell history and release history | Use Secrets created outside Helm |

---

# Part 14 — Command Cheat Sheet

| Command | What it does |
|---------|--------------|
| `helm create NAME` | Make a new starter chart |
| `helm lint ./chart` | Check for mistakes |
| `helm template NAME ./chart` | Show the finished YAML (no install) |
| `helm template NAME ./chart -s templates/x.yaml` | Show just one file |
| `helm install NAME ./chart` | Install the chart |
| `helm install NAME ./chart --dry-run=server` | Pretend install, checked by the cluster |
| `helm upgrade NAME ./chart` | Update an existing install |
| `helm upgrade --install NAME ./chart` | Install or upgrade, whichever is needed |
| `helm rollback NAME REVISION` | Go back to an earlier revision |
| `helm uninstall NAME` | Remove the release |
| `helm list -A` | See all releases everywhere |
| `helm status NAME` | Release summary |
| `helm history NAME` | List revisions |
| `helm get values NAME` | Values you set |
| `helm get manifest NAME` | The YAML that was applied |
| `helm test NAME` | Run chart tests |
| `helm show values CHART` | A chart's default values |
| `helm repo add NAME URL` | Add a chart repository |
| `helm repo update` | Refresh repository lists |
| `helm search repo WORD` | Search added repositories |
| `helm search hub WORD` | Search Artifact Hub |
| `helm dependency update ./chart` | Download subcharts |
| `helm package ./chart` | Make a `.tgz` |
| `helm push FILE.tgz oci://...` | Upload to an OCI registry |
| `helm registry login HOST` | Log in to an OCI registry |
| `helm pull CHART --untar` | Download a chart to look inside |

Useful flags: `-n NAMESPACE`, `--create-namespace`, `-f values.yaml`, `--set key=value`, `--version X`, `--wait`, `--timeout 5m`, `--debug`.

---

# Part 15 — Glossary

| Word | Simple meaning |
|------|---------------|
| **Ambient mode** | Istio without a sidecar in each pod |
| **Annotation** | A sticky note on a Kubernetes object (extra info for tools) |
| **Canary** | Sending a small slice of users to a new version first |
| **Chart** | A Helm package: templates + default values |
| **CRD** | Custom Resource Definition — teaches Kubernetes a new kind of object |
| **Gateway API** | The modern standard for letting outside traffic in |
| **Hook** | A job that runs at a special moment (before install, after upgrade...) |
| **JWT** | A signed digital badge that says who you are |
| **Label** | A name tag used to find and group objects |
| **mTLS** | Encryption where both sides prove who they are |
| **OCI registry** | A storage place for images and charts |
| **OIDC** | The standard way apps talk to login systems like Keycloak |
| **Release** | One installed copy of a chart |
| **Revision** | A numbered snapshot of a release |
| **Sidecar** | An extra helper container in the same pod |
| **Subchart** | A chart used inside another chart |
| **Template** | A file with `{{ }}` blanks |
| **Values** | The settings that fill the blanks |
| **Waypoint** | An Istio ambient proxy that handles advanced (L7) rules |

---

# Part 16 — Practice Exercises (with Answers)

Try each one before peeking!

### Exercise 1 — Add a new knob
Add a `backgroundColor` value (default `"white"`) and use it in the web page.

<details>
<summary>Answer</summary>

values.yaml:
```yaml
backgroundColor: "white"
```
configmap.yaml:
```yaml
  index.html: |
    <body style="background: {{ .Values.backgroundColor }}">
      <h1>{{ .Values.message }}</h1>
    </body>
```
Test: `helm template demo ./hello-web --set backgroundColor=lightblue`
</details>

### Exercise 2 — Make a value required
Make the chart fail with a helpful message if `message` is empty.

<details>
<summary>Answer</summary>

```yaml
<h1>{{ required "Please set .Values.message — what should the page say?" .Values.message }}</h1>
```
Test: `helm template demo ./hello-web --set message=""` → error with your message.
</details>

### Exercise 3 — Loop over a list
Add a `links` list of `{name, url}` and show them as links on the page.

<details>
<summary>Answer</summary>

values.yaml:
```yaml
links:
  - name: Helm Docs
    url: https://helm.sh/docs/
  - name: Artifact Hub
    url: https://artifacthub.io
```
configmap.yaml:
```yaml
    <ul>
    {{- range .Values.links }}
      <li><a href="{{ .url }}">{{ .name }}</a></li>
    {{- end }}
    </ul>
```
</details>

### Exercise 4 — Turn a file on and off
Make the ServiceAccount only appear when `serviceAccount.create` is true. Prove it works.

<details>
<summary>Answer</summary>

Wrap the file in `{{- if .Values.serviceAccount.create }} ... {{- end }}` (see Part 5.6).
```bash
helm template demo ./hello-web --set serviceAccount.create=false | grep ServiceAccount
# prints nothing ✅
```
</details>

### Exercise 5 — Fix the bug
What's wrong here?
```yaml
{{- range .Values.fruits }}
- name: {{ .Release.Name }}-{{ . }}
{{- end }}
```

<details>
<summary>Answer</summary>

Inside `range`, the dot is the current fruit, not the top level. Use `$`:
```yaml
- name: {{ $.Release.Name }}-{{ . }}
```
</details>

### Exercise 6 — Two environments
Install the chart twice in two namespaces with different messages.

<details>
<summary>Answer</summary>

```bash
helm upgrade --install hello -n dev  --create-namespace ./hello-web --set message="Dev!"
helm upgrade --install hello -n prod --create-namespace ./hello-web --set message="Prod!"
helm list -A
```
Same release name is OK because they're in different namespaces.
</details>

### Exercise 7 — Canary math
With the Istio template from Part 9, what weights do v1 and v2 get with `--set istio.canary.stableWeight=75`?

<details>
<summary>Answer</summary>

v1 gets 75, v2 gets `100 - 75 = 25`.
</details>

### 🏅 Challenge project
Build a chart called `school-portal` that:
1. Runs a web app with 2 replicas, probes, and resource requests
2. Has a ConfigMap with the checksum trick
3. Uses `existingSecret` for passwords
4. Has an optional HTTPRoute
5. Has optional Istio canary + JWT checking with Keycloak
6. Has a `values.schema.json`, a `helm test`, and a README
7. Passes `helm lint` with no warnings

---

# Part 17 — Next Steps

- 📘 Official Helm docs: https://helm.sh/docs/ (read "Chart Template Guide" and "Best Practices")
- 🔎 Find real charts to study: https://artifacthub.io
- 🚪 Gateway API: https://gateway-api.sigs.k8s.io
- 🕸️ Istio: https://istio.io/latest/docs/
- 🔑 Keycloak: https://www.keycloak.org/documentation
- 🧪 Practice: `helm pull <some-popular-chart> --untar`, then read its templates. Seeing how experts write charts is one of the best ways to learn.

**Remember the golden loop:**
> ✏️ Change a value → 👀 `helm template` → ✅ `helm lint` → 🚀 `helm upgrade` → 🔍 check → ↩️ `helm rollback` if needed

Start small, change one thing at a time, and always look at the output. You've got this! 🚀
