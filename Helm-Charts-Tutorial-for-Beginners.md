# Helm Charts Tutorial for Beginners
## (Explained Simply — Middle School Style)

**What you will learn:**
- What Helm is and why it helps
- How to create a Helm chart from scratch
- The most important parts of a chart
- How to deploy a simple web app
- How to add login (authentication) with Keycloak
- How to use Istio to control traffic and add security
- Some advanced tricks

---

## 1. The Big Picture (Easy Analogy)

Imagine your computer cluster (Kubernetes) is a **big school**.

- **Pods** = classrooms where the actual work happens (your apps run here)
- **Services** = hallways that let people find the right classroom
- **Deployments** = the school rules that say “always keep 3 copies of this classroom running”
- **Ingress / Gateway** = the front door of the school

Writing all these rules by hand for every app is boring and easy to mess up.  
**Helm is like a smart recipe book.** You write the recipe once (the chart). Then you can cook the same meal many times, change a few ingredients (values), and get a perfect result every time.

---

## 2. How a Helm Chart Is Created

### Step 1: Install Helm (if you don’t have it)
```bash
# On Mac
brew install helm

# On Linux
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
```

### Step 2: Create a new chart
```bash
helm create my-webapp
```

This command creates a folder that looks like this:

```
my-webapp/
├── Chart.yaml          ← The name tag of the recipe
├── values.yaml         ← The default ingredients
├── charts/             ← Other recipes this one needs (empty at first)
├── templates/          ← The actual instructions (YAML files with placeholders)
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── ingress.yaml
│   ├── NOTES.txt
│   └── _helpers.tpl    ← Helper functions
└── .helmignore
```

**Important things:**
1. **Chart.yaml** — tells Helm the name, version, and description of your chart.
2. **values.yaml** — the knobs you can turn (how many copies, which image to use, etc.).
3. **templates/** — the real Kubernetes files, but with special `{{ }}` placeholders so they can change based on the values.

---

## 3. The Most Important Files (With Examples)

### Chart.yaml
```yaml
apiVersion: v2
name: my-webapp
description: A simple web app that says hello
type: application
version: 1.0.0          # Version of the chart itself
appVersion: "1.0.0"     # Version of the app inside
```

### values.yaml (the knobs)
```yaml
replicaCount: 2

image:
  repository: nginx
  tag: "1.25"
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  port: 80

ingress:
  enabled: false
  host: myapp.example.com

resources:
  limits:
    cpu: 200m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

### templates/deployment.yaml (simplified)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "my-webapp.fullname" . }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app: {{ include "my-webapp.name" . }}
  template:
    metadata:
      labels:
        app: {{ include "my-webapp.name" . }}
    spec:
      containers:
        - name: web
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          ports:
            - containerPort: 80
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
```

**How the magic works:**  
Helm takes the template + the values and produces normal Kubernetes YAML.  
You can see the result without installing:
```bash
helm template my-webapp ./my-webapp
```

---

## 4. Basic Example — Simple Web App

1. Create the chart:
   ```bash
   helm create simple-web
   ```

2. Change `values.yaml` so it uses a fun image (for example a simple demo app).

3. Install it:
   ```bash
   helm install my-first-app ./simple-web
   ```

4. Check it:
   ```bash
   kubectl get pods
   helm list
   ```

5. Upgrade it later (change something in values and run):
   ```bash
   helm upgrade my-first-app ./simple-web --set replicaCount=3
   ```

6. Uninstall when done:
   ```bash
   helm uninstall my-first-app
   ```

**What’s important here?**  
You never edited the YAML by hand after the first time. You just changed the values.

---

## 4.5 Key Kubernetes Pieces (and How Helm Configures Them)

Think of Kubernetes as the school’s rule book. Helm is the person who fills in the blanks on those rules so everything works the way you want.

Here are the most important pieces you will see in almost every Helm chart.

### Deployment — “Keep this many copies running”
A **Deployment** tells Kubernetes: “Always keep X copies of my app running. If one dies, start a new one.”

**What it means:**  
Your app is packaged as a container (like a lunchbox with everything it needs). The Deployment is the rule that says how many lunchboxes to have and which picture (image) to use.

**How Helm configures it:**  
In `values.yaml` you set things like:
```yaml
replicaCount: 3
image:
  repository: myapp
  tag: "1.2.0"
```

In the template (`templates/deployment.yaml`) Helm fills in the blanks:
```yaml
spec:
  replicas: {{ .Values.replicaCount }}
  template:
    spec:
      containers:
        - name: app
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
```

You change one number in values and Helm updates the whole school rule.

### Service — “How other things find my app”
A **Service** is like a hallway sign. Pods come and go (they get new IP addresses), but the Service stays the same so other apps can always find yours.

**Common types:**
- `ClusterIP` — only other apps inside the school can reach it
- `NodePort` or `LoadBalancer` — people outside the school can reach it

**How Helm configures it:**
```yaml
# values.yaml
service:
  type: ClusterIP
  port: 80
  targetPort: 8080
```

```yaml
# templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "myapp.fullname" . }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: {{ .Values.service.targetPort }}
  selector:
    app: {{ include "myapp.name" . }}
```

### ConfigMap — “Settings that are not secret”
A **ConfigMap** holds plain text settings (like a class schedule or rules posted on the wall). Your app can read them as environment variables or as files.

**Example values:**
```yaml
config:
  LOG_LEVEL: info
  APP_MODE: production
  MAX_CONNECTIONS: "100"
```

**Template that creates the ConfigMap:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "myapp.fullname" . }}-config
data:
  {{- range $key, $value := .Values.config }}
  {{ $key }}: {{ $value | quote }}
  {{- end }}
```

Then the Deployment mounts it:
```yaml
envFrom:
  - configMapRef:
      name: {{ include "myapp.fullname" . }}-config
```

### Secret — “Passwords and private stuff”
A **Secret** is like a locked locker. It holds passwords, API keys, database credentials, etc. Kubernetes stores them encoded (base64), and Helm can create them from values or from existing secrets.

**Important:** Never put real passwords in Git. Prefer creating the Secret outside Helm or using a tool that injects them.

**Simple example in values (for learning only):**
```yaml
secret:
  DB_PASSWORD: "changeme"
  API_KEY: "abc123"
```

**Template:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "myapp.fullname" . }}-secret
type: Opaque
data:
  {{- range $key, $value := .Values.secret }}
  {{ $key }}: {{ $value | b64enc }}
  {{- end }}
```

In the Deployment you can use it the same way as a ConfigMap:
```yaml
envFrom:
  - secretRef:
      name: {{ include "myapp.fullname" . }}-secret
```

### Networking — “Who is allowed to talk to whom”
- **Ingress** or **Gateway** = the front door of the school (how people from the internet reach your app).
- **NetworkPolicy** = classroom rules about which apps can talk to each other.
- With **Istio** you get extra smart traffic rules (VirtualService, DestinationRule) that we already saw.

Helm usually has an `ingress` section in values.yaml:
```yaml
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: myapp.example.com
      paths:
        - path: /
          pathType: Prefix
```

The template turns that into a real Ingress resource.

### Sidecars — “Extra helpers that ride along”
A **sidecar** is an extra container that runs in the same Pod as your main app. It shares the network and can help with things like logging, security, or talking to other services.

**Common example: Istio’s Envoy sidecar**  
When you turn on Istio sidecar injection, every Pod automatically gets a little traffic cop (Envoy) that:
- Encrypts traffic (mTLS)
- Collects metrics
- Applies the VirtualService rules

**How Helm configures sidecars:**
1. You can add extra containers yourself in the Deployment template.
2. Or you just label the namespace so Istio injects them automatically:
   ```bash
   kubectl label namespace default istio-injection=enabled
   ```
3. In your chart you can control annotations:
   ```yaml
   podAnnotations:
     sidecar.istio.io/inject: "true"
   ```

**Manual sidecar example (for learning):**
```yaml
# in templates/deployment.yaml under containers:
- name: log-sidecar
  image: busybox
  command: ["sh", "-c", "while true; do echo $(date) >> /logs/app.log; sleep 30; done"]
  volumeMounts:
    - name: shared-logs
      mountPath: /logs
```

### How Helm ties everything together
When you run `helm install` or `helm upgrade`:
1. Helm reads `values.yaml` (and any `--set` or `-f` files).
2. It fills in every `{{ }}` placeholder in the templates.
3. It sends the finished YAML to Kubernetes.
4. Kubernetes creates or updates the Deployment, Service, ConfigMap, Secret, Ingress, etc.

You change values → Helm rewrites the rules → Kubernetes makes the school match the new rules.

---

## 5. Adding Authentication with Keycloak

**Keycloak** is like the school’s ID checker. Before anyone can enter certain apps, they must log in and get a special badge (token).

### Using a community Helm chart (recommended for beginners)
The popular community chart is from codecentric:

```bash
helm repo add codecentric https://codecentric.github.io/helm-charts
helm repo update

# Install Keycloak with a simple password (change it!)
helm install keycloak codecentric/keycloakx \
  --set keycloak.auth.adminUser=admin \
  --set keycloak.auth.adminPassword=ChangeMe123 \
  --set postgresql.enabled=true
```

After it starts, you can port-forward and open the admin console:
```bash
kubectl port-forward svc/keycloak-keycloakx-http 8080:80
# Then visit http://localhost:8080
```

### Making your own web app use Keycloak
In your web app’s Deployment, you usually set environment variables so the app knows where Keycloak is:

```yaml
env:
  - name: KEYCLOAK_URL
    value: "http://keycloak-keycloakx-http.default.svc.cluster.local"
  - name: KEYCLOAK_REALM
    value: "myrealm"
  - name: KEYCLOAK_CLIENT_ID
    value: "mywebapp"
```

Most modern apps (Spring Boot, Node, Python, etc.) have libraries that talk to Keycloak. You just point them at the URL above.

**Tip:** Never put the real admin password in `values.yaml` that you commit to Git. Use a Kubernetes Secret instead.

---

## 6. Using Istio (Service Mesh)

**Istio** is like extra traffic cops and security guards around the whole school.

- It can send 90% of visitors to version 1 of your app and 10% to version 2 (canary release).
- It can encrypt all traffic between apps automatically (mTLS).
- It can add retries, timeouts, and circuit breakers so one broken app doesn’t crash everything.

### Simple VirtualService example (canary traffic)
```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: my-webapp
spec:
  hosts:
    - my-webapp
  http:
    - route:
        - destination:
            host: my-webapp
            subset: v1
          weight: 90
        - destination:
            host: my-webapp
            subset: v2
          weight: 10
```

You put this file in your chart’s `templates/` folder (or apply it separately).

### DestinationRule (defines the subsets)
```yaml
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: my-webapp
spec:
  host: my-webapp
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
```

**Important:** Your Deployment must have the matching labels (`version: v1` or `version: v2`).

---

## 7. Advanced Examples

### A. Dependencies (subcharts)
In `Chart.yaml` you can list other charts your app needs:

```yaml
dependencies:
  - name: keycloakx
    version: "2.x.x"
    repository: "https://codecentric.github.io/helm-charts"
    condition: keycloak.enabled
```

Then run:
```bash
helm dependency update
```

### B. Helm Hooks (run something before or after install)
Example: create a secret or run a database migration before the app starts.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "my-webapp.fullname" . }}-migrate
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "-5"
    "helm.sh/hook-delete-policy": before-hook-creation
spec:
  # ... job that runs your migration
```

### C. Values schema (catch mistakes early)
Create `values.schema.json` so Helm rejects bad values before they hit the cluster.

### D. Multiple environments
```bash
helm install myapp ./my-webapp -f values-dev.yaml
helm install myapp ./my-webapp -f values-prod.yaml
```

### E. Packaging and sharing
```bash
helm package my-webapp
helm push my-webapp-1.0.0.tgz oci://myregistry.example.com/charts
```

---

## 8. What’s Most Important (Checklist)

1. **Always start with `helm create`** — it gives you a good skeleton.
2. **Put everything that can change in `values.yaml`**.
3. **Use `{{ .Release.Name }}` and `{{ .Release.Namespace }}`** so multiple copies can run side-by-side.
4. **Test with `helm template` and `helm lint`** before installing.
5. **Never put secrets in values.yaml** that you store in Git.
6. **Version your chart** (bump the version in Chart.yaml every time you change it).
7. **Document your values** in a README.md inside the chart.

---

## 9. Quick Commands You Will Use a Lot

| Command | What it does |
|---------|--------------|
| `helm create NAME` | Create a new chart |
| `helm template NAME ./chart` | Show the final YAML without installing |
| `helm lint ./chart` | Check for mistakes |
| `helm install NAME ./chart` | Install the chart |
| `helm upgrade NAME ./chart` | Update an existing install |
| `helm rollback NAME REVISION` | Go back to an earlier version |
| `helm uninstall NAME` | Remove everything |
| `helm list` | See what’s installed |
| `helm dependency update` | Download subcharts |

---

## 10. Next Steps

- Try the official Helm docs: https://helm.sh/docs/
- Look at real charts on Artifact Hub: https://artifacthub.io
- Practice with a small app that needs login (Keycloak) and traffic control (Istio)

You now know how Helm charts are built, why the pieces matter, and how to use them with Keycloak and Istio.  
Start small, change one value at a time, and use `helm template` to see what happens. You’ve got this!
