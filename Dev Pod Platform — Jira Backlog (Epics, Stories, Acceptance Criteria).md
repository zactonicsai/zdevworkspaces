# Dev Pod Platform — Jira Backlog (Epics, Stories, Acceptance Criteria)

Nov 8, 2026 · @Zachary Lewis

## Overview

This backlog stands up a Coder-style **Dev Pod Platform** on Kubernetes: a web portal where a developer signs in with Keycloak SSO, picks a tool image (Java, Node, Python, Go, …), and gets a running dev pod they can reach over SSH from VS Code Remote. A Spring Boot API does the work: it creates and deletes pods, tracks their state, and enforces who may touch what. Everything ships as Helm charts with Istio handling ingress and mTLS.

The backlog is in two phases. **Phase 1 (MVP, Epics 1–11)** delivers one base image, one tool image, the API, the portal, the Helm charts, Istio access, SSH and tests. **Phase 2 (Epics 12–13)** adds more tool images, catalog choices and advanced features. Stories are sized to be finished in 1–3 days each; split further if a story crosses two components.

&#91;embedded content: architecture · portal, API, Keycloak, Istio, bastion, dev pods\]

The browser and the SSH client both enter through the Istio gateway; the API (highlighted) is the only component that talks to the Kubernetes API, and Keycloak (tinted, already in place) issues the tokens the portal sends and the API verifies. Images come from the registry built in Epic 2.

### Epic map

| # | Epic | Phase | Primary owner | Depends on |
| --- | --- | --- | --- | --- |
| 1 | Foundations & repo setup | 1 | Platform | — |
| 2 | Base dev-pod container images | 1 | Platform | 1 |
| 3 | Keycloak SSO client & RBAC | 1 | Security / Platform | 1 |
| 4 | Spring Boot backend API | 1 | Backend | 1, 2, 3 |
| 5 | Web portal (frontend) | 1 | Frontend | 3, 4 |
| 6 | Helm charts | 1 | Platform | 2, 4, 5 |
| 7 | Istio, networking & SSH access | 1 | Platform | 6 |
| 8 | Security, quotas & hardening | 1 | Security / Platform | 4, 6, 7 |
| 9 | Observability & lifecycle ops | 1 | Platform / Backend | 4, 6 |
| 10 | Testing & QA | 1 | All | 4–7 |
| 11 | MVP release & documentation | 1 | All | 1–10 |
| 12 | Additional tool images & catalog | 2 | Platform | 11 |
| 13 | Advanced features | 2 | Backend / Frontend | 11 |

### Conventions used below

- Story key format: `DP-<epic>.<n>` (rename to your Jira project key).
- Each story has a one-line **As a / I want / so that** summary, a short task list, and **Acceptance criteria (AC)** written as Given/When/Then or checkable statements.
- **Definition of Done** for every story (see last section) applies in addition to the AC listed.
- Naming: namespace `devpods-system` for the platform, `devpods-<username>` (or a shared `devpods` namespace with labels) for user pods. Labels: `app.kubernetes.io/part-of=devpods`, `devpods.io/owner=<user-id>`, `devpods.io/image=<image-key>`.
- Registry path: `registry.example.com/devpods/<image>:<version>`.

### Assumptions and open questions

- [ ] Cluster already has Istio installed and an ingress gateway with a wildcard TLS cert (confirm cert source: cert-manager or external).
- [ ] Keycloak realm name and admin access to add a client (confirm who creates the client: platform team or IAM team).
- [ ] Storage class available for ReadWriteOnce PVCs (confirm name and default size).
- [ ] Network policy enforcement is active (Calico/Cilium) so NetworkPolicies take effect.
- [ ] Dev pods per user: one pod per user, or multiple? (Backlog assumes multiple, with a quota.)
- [ ] Persistence: ephemeral pods in MVP, persistent home volume in Phase 2 (DP-13.1). Confirm.

## Epic 1 — Foundations & repo setup

Goal: every later story has a repo, a pipeline, a registry and a namespace to land in. Nothing user-facing yet.

### DP-1.1 Create source repositories and branching model

As a platform engineer, I want separate repos so that images, API, portal and charts can version independently.

- Create repos: `devpods-images`, `devpods-api`, `devpods-portal`, `devpods-helm`, `devpods-docs` (or a monorepo with these folders — decide once).
- Add `README.md`, `CODEOWNERS`, `.editorconfig`, PR template, branch protection on `main`.
- Adopt trunk-based flow: short-lived feature branches, squash merge, Conventional Commits.

**AC**

- All repos exist with `main` protected (1 approval, status checks required).
- Each repo README states purpose, how to build locally, and how to release.
- Conventional Commit lint runs on PRs and blocks non-conforming titles.

### DP-1.2 Container registry project and credentials

As a platform engineer, I want a registry namespace so that images can be pushed by CI and pulled by the cluster.

- Create registry project `devpods/`; create a push robot account for CI and a read-only pull account.
- Store pull secret in the cluster namespaces via the secrets tool used by the org (Sealed Secrets, External Secrets, or Vault).
- Enable vulnerability scanning on push if the registry supports it.

**AC**

- CI can push a test image to `devpods/hello:test` and a pod in `devpods-system` can pull it.
- No registry credentials are stored in Git in plain text.
- Retention policy: keep last 10 tags per image, keep all semver tags.

### DP-1.3 Kubernetes namespaces, service accounts and base RBAC

As a platform engineer, I want the platform and user-pod namespaces created with least-privilege RBAC.

- Create `devpods-system` (platform) and `devpods` (user pods) namespaces with Istio injection label `istio-injection=enabled`.
- Create ServiceAccount `devpods-api` with a Role limited to the `devpods` namespace: pods, services, pvcs, secrets (named), configmaps: get/list/watch/create/delete; no cluster-admin.
- Apply Pod Security Admission labels: `devpods-system` = `restricted`, `devpods` = `baseline` (dev pods need a writable FS and SSH on a non-root port).

**AC**

- `kubectl auth can-i create pods -n devpods --as=system:serviceaccount:devpods-system:devpods-api` returns yes; same for `-n default` returns no.
- Namespaces carry the PSA and Istio labels; a privileged pod manifest is rejected in `devpods-system`.
- RBAC manifests live in `devpods-helm` (not applied by hand).

### DP-1.4 CI pipeline skeleton (build, test, scan, push)

As a developer on the platform, I want a standard pipeline so that every repo builds the same way.

- Create reusable pipeline templates (GitHub Actions / GitLab CI / Jenkins shared lib) for: Docker build with BuildKit cache, Trivy scan, unit tests, Helm lint, push on tag.
- Images tagged `<semver>` and `<git-sha>`; `latest` only on `main`.
- Sign images with cosign (keyless OIDC) and publish SBOM (syft) as an artifact.

**AC**

- A PR in `devpods-images` runs build + Trivy and fails on CRITICAL CVEs with a fix available.
- Merging to `main` pushes `<git-sha>` tag; tagging `v0.1.0` pushes `0.1.0` and signs it.
- `cosign verify` succeeds against the pushed image.

### DP-1.5 Local development environment (kind/minikube + Istio)

As a developer, I want a one-command local cluster so that I can test charts without the shared cluster.

- Script `make local-up` creates a kind cluster, installs Istio (demo profile), metrics-server, a local registry and a Keycloak dev instance (docker-compose or Helm).
- Document ports, hosts-file entries and how to tear down.

**AC**

- `make local-up` completes in under 10 minutes on a laptop and `istioctl verify-install` passes.
- `make local-down` removes everything.
- Documented in `devpods-docs/local-dev.md`.

### DP-1.6 Architecture decision records (ADRs)

As a team, I want decisions recorded so that reviewers understand why.

- Add `docs/adr/` with template; write ADR-001 namespace model (per-user vs shared), ADR-002 SSH exposure method (Istio TCP vs NodePort vs LoadBalancer), ADR-003 state store (Postgres vs CRD), ADR-004 image base (Ubuntu vs Debian vs UBI).

**AC**

- Four ADRs merged with status Accepted; each lists options considered and consequences.

## Epic 2 — Base dev-pod container images

Goal: one hardened base image every tool image builds on, plus the first tool image, so the API and Helm chart have something real to launch. Images are the pre-step for the Helm work.

### DP-2.1 Base image: OS, non-root user, SSH server

As a developer, I want a minimal, secure base so that every dev pod behaves the same.

- Dockerfile `images/base/Dockerfile` from a pinned LTS base (Ubuntu 24.04 or UBI 9 per ADR-004), multi-stage.
- Create user `dev` (UID 1000, GID 1000), home `/home/dev`, passwordless sudo only if ADR allows (default: no sudo).
- Install `openssh-server`, `git`, `curl`, `ca-certificates`, `zsh`/`bash`, `tmux`, `vim`, `locales`, `tzdata`.
- sshd config: listen on 2222 (non-root port), `PasswordAuthentication no`, `PermitRootLogin no`, `PubkeyAuthentication yes`, `AuthorizedKeysFile /home/dev/.ssh/authorized_keys`, `ClientAliveInterval 60`.
- Host keys generated at first start into an emptyDir/PVC path (not baked into the image).
- Entrypoint script: writes authorized key(s) from env/secret mount, generates host keys if missing, starts sshd in foreground, runs optional `/etc/devpod/init.d/*.sh`.
- `HEALTHCHECK` / readiness: TCP check on 2222.
- OCI labels (`org.opencontainers.image.*`), `.dockerignore`, hadolint clean.

**AC**

- `docker run -p 2222:2222 -e AUTHORIZED_KEYS="$(cat ~/.ssh/id_ed25519.pub)" devpods/base` allows `ssh -p 2222 dev@localhost` with key only; password login is rejected.
- Image runs as UID 1000 (`docker inspect` shows `User: 1000`); `whoami` in the pod is `dev`.
- Host keys differ between two fresh containers (not baked in).
- Trivy: 0 CRITICAL, 0 HIGH with fix available. hadolint passes. Image size under 400 MB.
- Image signed and SBOM published by DP-1.4 pipeline.

### DP-2.2 Dev-pod init contract (env vars, mounts, init hooks)

As a backend developer, I want a documented contract so that the API can configure any image the same way.

- Document in `images/CONTRACT.md`: env vars (`DEVPOD_USER`, `DEVPOD_OWNER_ID`, `DEVPOD_NAME`, `AUTHORIZED_KEYS` or mount `/etc/devpod/ssh/authorized_keys`, `GIT_USER_NAME`, `GIT_USER_EMAIL`, `TZ`), mount points (`/home/dev` workspace, `/etc/devpod/ssh`, `/etc/devpod/init.d`), ports (2222 SSH, 8080–8089 reserved for app previews), signals/shutdown (SIGTERM graceful 30 s).
- Base image implements the contract; a `contract-test.sh` verifies it.

**AC**

- `CONTRACT.md` merged and linked from each tool image README.
- `contract-test.sh` passes against `devpods/base` in CI and fails if sshd is not listening within 10 s.

### DP-2.3 First tool image: `devpods/java` (JDK + Maven + Gradle)

As a Java developer, I want a ready pod so that I can build Spring projects immediately.

- `FROM devpods/base:<version>`; install Temurin JDK 21 (and 17 via SDKMAN or multiple JDK dirs), Maven 3.9, Gradle 8, `jq`, `docker` CLI (client only, optional), `kubectl`.
- Pre-warm Maven/Gradle caches with a common BOM? (optional; measure size tradeoff).
- Set `JAVA_HOME`, `PATH`, `MAVEN_OPTS` defaults; `~/.m2/settings.xml` template pointing to the org mirror if any.

**AC**

- `java -version`, `mvn -v`, `gradle -v` succeed as user `dev`.
- `mvn archetype:generate` of a sample project and `mvn package` succeed inside the container.
- Passes `contract-test.sh`; Trivy gate same as base. Size under 1.2 GB.

### DP-2.4 Image catalog metadata (`catalog.yaml`)

As the portal and API, I want a machine-readable catalog so that choices are data, not code.

- Define `catalog.yaml` schema: `key`, `displayName`, `description`, `image`, `tags`, `defaultResources` (cpu/mem requests+limits), `storageGB`, `ports`, `icon`, `enabled`, `minRole`.
- Entries for `base` and `java`. JSON Schema file + CI validation.
- Publish catalog as a ConfigMap via the Helm chart (DP-6.x) and expose via API `GET /api/v1/catalog` (DP-4.x).

**AC**

- Invalid catalog (missing `image`) fails CI with a readable error.
- Catalog lists both images with resources; API returns the same entries.

### DP-2.5 Image versioning, promotion and rebuild schedule

As a platform engineer, I want images rebuilt regularly so that CVEs are patched.

- Weekly scheduled pipeline rebuilds all images from the base with `apt upgrade`, retags, scans.
- Promotion: `dev` → `stable` tag after automated contract test passes; catalog references `stable`.
- Changelog generated from commits.

**AC**

- Scheduled job runs Monday 02:00 and opens a PR/notification on scan failures.
- Catalog `stable` tag only moves after `contract-test.sh` passes.

### DP-2.6 Image hardening checklist

As a security engineer, I want the images to meet the baseline.

- No secrets in layers (`docker history` check, gitleaks on repo).
- Read-only root FS compatible: writable paths limited to `/home/dev`, `/tmp`, `/var/run/sshd`, `/etc/ssh` (host keys dir).
- Drop all capabilities except `NET_BIND_SERVICE` if needed (not needed on 2222).
- `seccompProfile: RuntimeDefault` works.

**AC**

- Pod runs with `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, `runAsNonRoot: true` and SSH still works.
- gitleaks and `docker history` checks pass in CI.

## Epic 3 — Keycloak SSO client & RBAC model

Goal: reuse the existing Keycloak realm; add one client for the portal (public, PKCE) and have the API validate the same tokens. Roles decide who can create pods and who can administer.

### DP-3.1 Define roles and authorization model

As a product owner, I want roles defined so that access rules are explicit before code is written.

- Realm/client roles: `devpods-user` (create/manage own pods), `devpods-admin` (manage all pods, catalog, quotas), `devpods-viewer` (read-only, optional).
- Decide mapping: Keycloak groups → roles (e.g. group `/engineering` gets `devpods-user`).
- Ownership rule: a pod belongs to the `sub` claim of the creating user; only owner or admin may stop/delete.
- Write `docs/authz.md` with a permission matrix (action × role).

**AC**

- Permission matrix approved by security and covers: list catalog, create pod, list own pods, list all pods, stop/start, delete, get SSH info, edit catalog, edit quotas.
- Role names match exactly what DP-3.2 creates.

### DP-3.2 Create Keycloak client for the portal (OIDC, PKCE)

As a platform engineer, I want a Keycloak client so that the portal can log users in.

- Client `devpods-portal`: public client, Standard Flow on, Direct Access Grants off, PKCE S256 required, Implicit off.
- Valid redirect URIs: `https://devpods.example.com/*` and local dev `http://localhost:5173/*`; Web Origins restricted to the portal host (no `*`).
- Access token lifespan 5 min, refresh via SSO session; `devpods-api` audience mapper so tokens carry `aud: devpods-api`.
- Role mapper: client roles in token under `resource_access.devpods-api.roles` (or realm roles — match DP-3.1).
- Export client as JSON into `devpods-helm/keycloak/` and (if available) apply with Keycloak Operator CR or Terraform for repeatability.

**AC**

- A user in the test group logs in via the portal redirect and receives an ID token and access token; `aud` includes `devpods-api`; roles claim contains `devpods-user`.
- Redirect to an unregistered URI is rejected by Keycloak.
- Client configuration is reproducible from the exported JSON/CR (applied to local Keycloak in DP-1.5).

### DP-3.3 API resource server configuration (token validation)

As a backend developer, I want the API to validate JWTs against Keycloak so that only SSO users call it.

- Spring Security OAuth2 Resource Server with `issuer-uri`; JWK cache; validate `iss`, `aud`, `exp`.
- Map roles claim to `ROLE_DEVPODS_USER` / `ROLE_DEVPODS_ADMIN` authorities via a `JwtAuthenticationConverter`.
- Reject tokens without `devpods-api` audience.

**AC**

- `GET /api/v1/me` with a valid token returns `sub`, `preferred_username`, `email`, roles; with no token returns 401; with wrong audience returns 401.
- Admin-only endpoint returns 403 for a `devpods-user` token.
- Unit tests cover the converter with sample tokens.

### DP-3.4 Service account for API → Keycloak admin lookups (optional)

As the API, I want to resolve usernames and groups so that admins can search users when assigning pods.

- Confidential client `devpods-api-sa` with `view-users` role only; secret stored in K8s Secret.
- Used only for admin UI user search; not for user actions.

**AC**

- `GET /api/v1/admin/users?q=ja` returns matching users for admins; 403 for users.
- Secret rotation documented; service account has no `manage-users`.

### DP-3.5 SSH public-key management via portal

As a developer, I want to register my SSH public key once so that every pod I create accepts it.

- Decide storage: Keycloak user attribute `ssh_public_keys` vs API database (recommend API DB, shown in profile page).
- Validate key format (ed25519/rsa ≥ 3072), max 5 keys, label per key.

**AC**

- Adding a malformed key returns 400 with a clear message.
- Keys appear in the pod's `authorized_keys` on creation (verified by DP-7.x SSH test).
- Removing a key and restarting the pod revokes access.

## Epic 4 — Spring Boot backend API

Goal: a stateless REST API (Spring Boot 3.x, Java 21) that creates, lists, stops and deletes dev pods through the Kubernetes API, keeps pod state in Postgres, and enforces ownership via the Keycloak token. Pod state machine: `REQUESTED → PROVISIONING → RUNNING → STOPPING → STOPPED → DELETING → DELETED`, plus `FAILED`.

### DP-4.1 Project skeleton, build and quality gates

- Spring Boot 3.x, Java 21, Maven; modules/packages: `api`, `domain`, `k8s`, `persistence`, `security`, `catalog`.
- Dependencies: Web, Security OAuth2 Resource Server, Data JPA, Flyway, Validation, Actuator, Micrometer Prometheus, fabric8 kubernetes-client (or official client), springdoc-openapi, Testcontainers.
- Dockerfile (distroless or Temurin JRE, non-root), Spotless/Checkstyle, JaCoCo ≥ 80% on `domain`.

**AC**

- `mvn verify` runs unit tests, format check and coverage gate; image builds in CI (DP-1.4).
- `GET /actuator/health` returns UP; `/v3/api-docs` and Swagger UI load (disabled in prod profile).

### DP-4.2 Configuration and profiles

- Profiles `local`, `dev`, `prod`; all secrets via env vars; config properties class `DevpodsProperties` (namespace, default image pull secret, SSH host, storage class, quotas).
- Config validated at startup (`@Validated`), fails fast on missing required values.

**AC**

- Starting without `KEYCLOAK_ISSUER_URI` fails with a clear message; all properties documented in `README.md`.

### DP-4.3 Persistence: Postgres schema and migrations

- Tables: `dev_pod` (id, owner\_sub, owner\_username, name, image\_key, image\_ref, status, cpu, memory, storage\_gb, namespace, k8s\_pod\_name, ssh\_port/service info, created\_at, updated\_at, last\_activity\_at, ttl\_at), `ssh_key` (id, owner\_sub, label, public\_key, fingerprint, created\_at), `audit_event` (id, actor\_sub, action, target\_id, detail json, at).
- Flyway migrations; indexes on `owner_sub`, `status`.
- Helm deploys Postgres (Bitnami chart or operator) for dev; prod points to managed DB.

**AC**

- Migrations apply cleanly on empty DB and are idempotent on rerun; Testcontainers integration test boots Postgres and runs them.

### DP-4.4 Kubernetes client wrapper and in-cluster auth

- `K8sPodService` interface with fabric8 implementation: `createDevPod(spec)`, `deletePod`, `getPodStatus`, `listByLabel`, `scaleStop/Start` (see DP-4.8).
- In-cluster ServiceAccount token (DP-1.3); local profile uses kubeconfig.
- Timeouts, retries with backoff for transient API errors; map K8s errors to domain exceptions.

**AC**

- Integration test against kind (DP-1.5) creates and deletes a pod via the service.
- Unit tests with fabric8 mock server cover create, 409 conflict, 403 forbidden.

### DP-4.5 Dev pod manifest builder

- Build the pod (or StatefulSet of 1 — decide; StatefulSet gives stable name + PVC) from catalog entry + user request: labels/annotations from conventions, security context from DP-2.6, resources from catalog with user override within limits, `AUTHORIZED_KEYS` from the user's keys via a per-pod Secret, PVC for `/home/dev` (optional in MVP), Service (ClusterIP) exposing 2222.
- Istio sidecar annotations: `sidecar.istio.io/inject`, `traffic.sidecar.istio.io/excludeInboundPorts: "2222"` only if ADR-002 routes SSH outside the mesh.
- Pod name: `dp-<username>-<short-id>`, DNS-safe, max 63 chars.

**AC**

- Golden-file tests: given a catalog entry and request, the generated YAML matches the expected manifest.
- Generated pod passes the `baseline` PSA level and the admission policies from DP-8.x.
- Requesting resources above catalog max returns 400.

### DP-4.6 REST endpoints: catalog and pod CRUD

| Method | Path | Role | Purpose |
| --- | --- | --- | --- |
| GET | `/api/v1/me` | user | Current user, roles, quota usage |
| GET | `/api/v1/catalog` | user | Enabled images with resources |
| POST | `/api/v1/pods` | user | Create pod `{name, imageKey, cpu?, memory?, storageGb?}` → 202 + pod |
| GET | `/api/v1/pods` | user | List own pods (admin: `?all=true`) |
| GET | `/api/v1/pods/{id}` | owner/admin | Pod detail incl. status, events, ssh info |
| POST | `/api/v1/pods/{id}/stop` | owner/admin | Stop (scale to 0, keep PVC) |
| POST | `/api/v1/pods/{id}/start` | owner/admin | Start |
| DELETE | `/api/v1/pods/{id}` | owner/admin | Delete pod and resources |
| GET | `/api/v1/pods/{id}/ssh` | owner | SSH connection snippet |
| GET | `/api/v1/pods/{id}/logs` | owner/admin | Last N lines of container logs |
| GET/POST/DELETE | `/api/v1/ssh-keys` | user | Manage keys (DP-3.5) |
| GET/PUT | `/api/v1/admin/catalog` | admin | Edit catalog overrides |
| GET/PUT | `/api/v1/admin/quotas` | admin | Per-user/group quotas |

- Bean Validation on request bodies; RFC 7807 Problem Details for errors; OpenAPI annotated.
- Idempotency: POST `/pods` with same `name` for same owner returns 409.

**AC**

- Each row above has a MockMvc/WebTestClient test for 200/202, 400, 401, 403, 404 as applicable.
- A user cannot see or act on another user's pod (404 or 403 — pick one, document it).
- OpenAPI spec published as a build artifact and used by the portal client generator.

### DP-4.7 Pod state reconciliation (watcher)

- Background `@Scheduled` reconciler or fabric8 informer watches pods with `app.kubernetes.io/part-of=devpods`; updates DB status from pod phase and readiness; records `FAILED` with reason (ImagePullBackOff, OOMKilled, quota).
- Drift handling: pod exists in K8s but not DB → flag orphan; DB says RUNNING but pod gone → mark STOPPED/FAILED.
- Leader election (Spring Integration lock or K8s Lease) so only one replica reconciles.

**AC**

- Deleting a pod with `kubectl` is reflected in `GET /pods/{id}` within 30 s.
- ImagePullBackOff shows as `FAILED` with the K8s reason text.
- Two API replicas: only one holds the lease (verified in logs).

### DP-4.8 Stop/start semantics and resource release

- Stop = delete the Pod (or scale StatefulSet to 0) but keep PVC and Service; start recreates with the same name and keys.
- Ephemeral pods (no PVC) show a warning that stop loses data.

**AC**

- Stop then start preserves `/home/dev` when PVC is enabled; status transitions logged in `audit_event`.

### DP-4.9 Quotas and limits enforcement

- Per-user defaults: max 2 running pods, 4 vCPU, 8 GiB total; admins may override per user/group.
- Enforce in API before calling K8s; also set namespace `ResourceQuota` and `LimitRange` (DP-8.x) as backstop.

**AC**

- Third pod request returns 429/409 with Problem Details naming the quota; admin override allows it.

### DP-4.10 Audit log and events

- Every mutating call writes `audit_event` (actor, action, target, result, IP, user agent).
- Pod events from K8s stored or proxied for `GET /pods/{id}` detail.

**AC**

- Create, stop, start, delete and key changes appear in audit with correct actor; admins can `GET /api/v1/admin/audit?user=`.

### DP-4.11 Observability instrumentation

- Micrometer metrics: `devpods_pods_total{status}`, `devpods_create_duration_seconds`, `devpods_k8s_errors_total`; structured JSON logs with `traceId`; OpenTelemetry tracing exporter configurable.

**AC**

- `/actuator/prometheus` exposes the metrics; a create request produces a trace spanning API → K8s call.

### DP-4.12 API container image and Helm values contract

- Dockerfile with non-root user, `JAVA_TOOL_OPTIONS` for container memory, readiness/liveness via Actuator.
- Document every env var the chart must set (DP-6.2).

**AC**

- Image passes Trivy gate; starts under `restricted` PSA; readiness goes UP within 30 s.

## Epic 5 — Web portal (frontend)

Goal: a single-page app (React + TypeScript + Vite recommended; Angular acceptable) served by nginx, logging in via Keycloak (OIDC + PKCE) and calling the API with the bearer token. Keep it thin: all rules live in the API.

### DP-5.1 Portal project skeleton and build

- Vite + React + TS, ESLint/Prettier, Vitest, Playwright; UI kit (MUI/Chakra/shadcn — pick one); routing; env-driven config (`VITE_API_URL`, `VITE_KC_URL`, `VITE_KC_REALM`, `VITE_KC_CLIENT_ID`) injected at container start via `config.js` so one image serves all environments.
- Dockerfile: multi-stage, nginx unprivileged image, non-root, port 8080, security headers (CSP, HSTS, X-Frame-Options).

**AC**

- `npm run build` and `npm test` pass in CI; image passes Trivy gate; `curl -I` shows CSP and HSTS headers.
- Changing `VITE_API_URL` via env var at runtime takes effect without rebuilding.

### DP-5.2 Login/logout with Keycloak (OIDC PKCE)

- `keycloak-js` or `oidc-client-ts`; silent refresh; token stored in memory (not localStorage); logout clears SSO session.
- Route guard: unauthenticated → redirect to Keycloak; role-less user sees a “no access” page.

**AC**

- User logs in and sees their username and roles from `/api/v1/me`; refresh keeps the session; logout returns to Keycloak login.
- A user without `devpods-user` role sees an access-denied page with contact info.

### DP-5.3 Typed API client from OpenAPI

- Generate client (`openapi-typescript` + fetch wrapper) from the API spec artifact (DP-4.6); attach bearer token; central error handling for Problem Details.

**AC**

- Client regenerates in CI when the spec changes; a 401 triggers token refresh then retry once; 4xx errors show the Problem Details `detail` in a toast.

### DP-5.4 Image catalog page

- Cards from `GET /catalog`: icon, name, description, tags, default CPU/memory/storage; search and tag filter; disabled entries hidden.

**AC**

- Both MVP images display with correct resources; filter by tag `java` shows only the Java image.

### DP-5.5 Create pod dialog

- Form: name (DNS-safe validation, auto-suggest), image, CPU/memory/storage selectors bounded by catalog max and quota, optional TTL; shows quota remaining; submit → 202 → navigate to pod detail with live status.

**AC**

- Invalid name shows inline error before submit; exceeding quota disables submit with reason; successful create shows status `PROVISIONING` then `RUNNING` without page reload (polling every 3 s or SSE).

### DP-5.6 My pods list and pod detail

- Table: name, image, status badge, CPU/mem, created, last activity, actions (Start/Stop/Delete with confirm).
- Detail: status timeline, K8s events, recent logs (tail), resource usage if metrics available, SSH panel (DP-5.7).

**AC**

- Stop/Start/Delete reflect within 5 s; delete requires typing the pod name; `FAILED` shows the reason text.

### DP-5.7 SSH connection panel

- Shows copyable `ssh` command and an `~/.ssh/config` snippet (host alias, port, user `dev`, `ProxyCommand` if a bastion/Istio TCP approach is used), plus a “Connect with VS Code” deep link `vscode://vscode-remote/ssh-remote+<alias>/home/dev`.
- Warns if the user has no SSH key registered and links to the profile page.

**AC**

- Copying the snippet and pasting into `~/.ssh/config` lets `ssh <alias>` connect (verified in DP-10.x e2e).
- VS Code link opens Remote-SSH to the pod on a machine with the extension installed.

### DP-5.8 Profile page: SSH keys

- List/add/delete keys with label and fingerprint; paste validation.

**AC**

- Adding an invalid key shows the API error; fingerprint matches `ssh-keygen -lf`.

### DP-5.9 Admin pages

- All pods (filter by user/status), force stop/delete, quotas editor, catalog overrides (enable/disable image, change defaults). Visible only to `devpods-admin`.

**AC**

- Non-admins get no admin menu and a direct URL returns the access-denied page; admin changes to quota take effect on the next create.

### DP-5.10 Accessibility, responsiveness and UX polish

- WCAG 2.1 AA via axe in Playwright; keyboard navigation; dark mode optional; empty states and loading skeletons.

**AC**

- axe reports 0 serious/critical violations on catalog, create, list, detail pages; usable at 1024 px width.

## Epic 6 — Helm charts

Goal: two charts in `devpods-helm`: **`devpods-platform`** (portal + API + Postgres dependency + Istio routing + RBAC + catalog ConfigMap) installed once per cluster, and **`devpods-devpod`** (the per-user pod template) that the API renders or that admins can use for manual testing. Charts are published to an OCI registry and lint/tested in CI.

### DP-6.1 Chart scaffolding, repo layout and publishing

- `charts/devpods-platform`, `charts/devpods-devpod`; `Chart.yaml` with semver, `values.yaml` fully commented, `values.schema.json`, `NOTES.txt`, `README.md` generated by helm-docs.
- CI: `helm lint`, `helm unittest`, `kubeconform` against the cluster K8s version, `ct install` on kind; publish `oci://registry.example.com/devpods/charts` on tag.

**AC**

- `helm install --dry-run` with default values succeeds; `helm-docs` output is committed and up to date (CI fails otherwise); chart pulls via `helm pull oci://.../devpods-platform --version x.y.z`.

### DP-6.2 Platform chart: API Deployment, Service, config and secrets

- Deployment (2 replicas, PDB, rolling update), Service, ServiceAccount + Role/RoleBinding (DP-1.3), ConfigMap for non-secret config, Secret references (DB URL, Keycloak client secret for DP-3.4) via existingSecret pattern, HPA optional, resources set, probes to Actuator, `securityContext` restricted, topology spread.

**AC**

- All env vars from DP-4.12 are set from values; `helm unittest` asserts probes, securityContext and SA name; pod becomes Ready on kind.

### DP-6.3 Platform chart: portal Deployment and runtime config

- Deployment + Service for nginx image; ConfigMap rendering `config.js` from values (API URL, Keycloak URL/realm/client); probes on `/healthz`.

**AC**

- Changing `portal.keycloak.clientId` in values changes `config.js` on upgrade with a rolling restart (checksum annotation).

### DP-6.4 Platform chart: Postgres dependency and external DB mode

- Bitnami PostgreSQL subchart (`postgresql.enabled=true`) for dev; `externalDatabase.*` values for prod; Flyway runs on API start.

**AC**

- Both modes install; with `postgresql.enabled=false` no DB resources are created and API connects to the external URL.

### DP-6.5 Platform chart: Istio Gateway and VirtualService for portal/API

- Gateway (or reuse shared ingress gateway via values), VirtualService: `devpods.example.com/` → portal, `/api/` → API, HTTPS redirect, timeouts, CORS not needed (same host). DestinationRules with `ISTIO_MUTUAL`. Optional `RequestAuthentication` + `AuthorizationPolicy` requiring a valid Keycloak JWT on `/api/*` at the mesh edge (defense in depth; API still validates).

**AC**

- `curl https://devpods.example.com/api/v1/catalog` without token returns 401 from Istio (if enabled) or API; portal loads over HTTPS; `istioctl analyze -n devpods-system` is clean.

### DP-6.6 Platform chart: catalog ConfigMap and RBAC for user namespace

- Render `catalog.yaml` (DP-2.4) into a ConfigMap mounted by the API; create `devpods` namespace resources: ResourceQuota, LimitRange, NetworkPolicies (DP-8.x), image pull secret, PeerAuthentication STRICT.

**AC**

- Upgrading the chart with a changed catalog updates the API catalog within 60 s (hot reload or restart); quota objects exist in `devpods`.

### DP-6.7 Dev-pod chart (`devpods-devpod`)

- Template a single dev pod: StatefulSet (1 replica) or Pod + optional PVC, Service (ClusterIP :2222), per-pod Secret for `authorized_keys`, labels/annotations from conventions, Istio sidecar settings, securityContext from DP-2.6, resources from values, optional `initContainers` for chown of PVC.
- Values: `owner`, `image`, `resources`, `storage.enabled/size/class`, `sshPublicKeys[]`, `ttlHours`, `extraEnv`, `extraVolumes`.
- API either shells out to `helm template` (simple) or mirrors the manifests in Java (DP-4.5) — the chart is the **source of truth**; golden tests in DP-4.5 compare the two.

**AC**

- `helm install dp-test charts/devpods-devpod --set image=devpods/java:stable --set sshPublicKeys[0]="..."` produces a Ready pod on kind and `kubectl port-forward svc/dp-test 2222:2222` allows SSH as `dev`.
- `helm unittest` asserts securityContext, labels, port 2222 and no `latest` tag.

### DP-6.8 Istio configuration for SSH to dev pods

- Per ADR-002 implement one of: (a) Istio ingress gateway TCP port (e.g. 2222) + per-pod VirtualService with SNI/TLS routing — requires `ssh -o ProxyCommand=openssl s_client` or an `sshpiper`/bastion; (b) a shared **SSH bastion/jump pod** in `devpods-system` exposed via Istio TCP, which forwards to `dp-<name>.devpods.svc:2222` based on the SSH user/host alias; (c) per-pod NodePort/LoadBalancer (simplest, least scalable). Recommended: (b) bastion with `ProxyJump`.
- Chart renders the Gateway TCP server, VirtualService/TCPRoute and bastion Deployment; `excludeInboundPorts` on 2222 if sidecar interferes with sshd.

**AC**

- From a laptop outside the cluster, `ssh -J bastion@devpods-ssh.example.com:2222 dev@dp-<name>` connects; mTLS between bastion and pod enforced (PeerAuthentication STRICT) or port 2222 explicitly excluded and documented.
- `istioctl analyze` clean; connection survives 30 minutes idle with keepalives.

### DP-6.9 Environment values files and GitOps hookup

- `values-dev.yaml`, `values-stage.yaml`, `values-prod.yaml`; Argo CD Application / Flux HelmRelease manifests; secrets via External Secrets or Sealed Secrets.

**AC**

- Argo CD syncs `devpods-platform` to the dev cluster from `main`; drift shows in Argo; no plaintext secret in Git.

## Epic 7 — Istio, networking & SSH access

Goal: developers reach the portal over HTTPS and their pods over SSH from VS Code Remote, with the mesh enforcing mTLS and the SSH path locked to key auth and the owner's pod only.

### DP-7.1 DNS and TLS certificates

- DNS records `devpods.example.com` (HTTPS) and `devpods-ssh.example.com` (TCP 2222) → Istio ingress gateway LB; cert via cert-manager `Certificate` or existing wildcard; Gateway references the secret.

**AC**

- `openssl s_client -connect devpods.example.com:443` shows a valid chain; renewal tested by shortening duration in dev.

### DP-7.2 SSH bastion / jump service

- Deployment of a hardened OpenSSH bastion (or `sshpiper`) in `devpods-system`: key-only, no shell (`ForceCommand` / `PermitTTY no`, `AllowTcpForwarding yes` restricted to `devpods` namespace via `PermitOpen` or network policy), fail2ban-like rate limiting via Istio `connection limits` or `sshd MaxStartups`.
- Bastion authorized keys: option A — bastion calls API `GET /internal/ssh-keys?fingerprint=` via `AuthorizedKeysCommand` (recommended, keys always current); option B — Secret synced by API.
- Pod reachability: bastion forwards to `dp-<name>.devpods.svc.cluster.local:2222`; the API's `/pods/{id}/ssh` returns the exact `ProxyJump` snippet.

**AC**

- A registered user connects with `ssh -J devpods-ssh.example.com:2222 dev@dp-<name>`; an unregistered key is refused at the bastion; the bastion refuses interactive shell; a user cannot forward to another user's pod (verified: attempt returns `administratively prohibited`).
- Bastion image passes Trivy; runs non-root with `restricted` PSA.

### DP-7.3 Istio TCP ingress for SSH

- Gateway `server: port 2222 TCP`, VirtualService `tcp` route to bastion; `AuthorizationPolicy` allows only ingress gateway → bastion on 2222; bastion → dev pods on 2222.

**AC**

- `nc -zv devpods-ssh.example.com 2222` succeeds; direct connection from any other namespace to a dev pod's 2222 is denied by AuthorizationPolicy/NetworkPolicy.

### DP-7.4 Dev pod SSH host-key handling and `known_hosts` UX

- Since pods are recreated, host keys change; options: persist host keys in the pod's PVC/Secret (recommended) so the fingerprint is stable per pod; portal shows the fingerprint; snippet includes `StrictHostKeyChecking=accept-new` guidance.

**AC**

- Stop/start of a pod keeps the same host key fingerprint; portal fingerprint matches `ssh-keyscan`.

### DP-7.5 VS Code Remote-SSH validation

- Document and verify Remote-SSH: server install into `/home/dev/.vscode-server`, extensions, port forwarding of 8080 from pod to laptop via SSH `-L`.

**AC**

- VS Code opens `/home/dev` on the pod, terminal works, a Spring Boot app on 8080 is reachable via forwarded port; documented with screenshots in `devpods-docs/vscode.md`.

### DP-7.6 Egress policy for dev pods

- Decide egress: allow internet (package downloads) but deny cluster-internal except DNS, registry mirror, Git server; implement with NetworkPolicy and Istio `Sidecar`/`ServiceEntry` (REGISTRY\_ONLY optional).

**AC**

- From a dev pod: `curl https://repo1.maven.org` works; `curl http://devpods-api.devpods-system:8080` and `curl` to kube-apiserver are denied; documented exceptions list.

### DP-7.7 Optional: app preview URLs through Istio (Phase 1 stretch)

- Route `https://<pod>-8080.devpods.example.com` → pod port 8080 with Keycloak JWT check at the gateway (`RequestAuthentication` + `AuthorizationPolicy` with owner claim match) so a developer can share a running app.

**AC**

- Owner opens preview URL after SSO and sees the app; another user receives 403.

## Epic 8 — Security, quotas, policies and hardening

Goal: a developer with a dev pod cannot escape it, starve others, or reach what they should not; the platform passes a security review before MVP release.

### DP-8.1 Namespace ResourceQuota and LimitRange

- `devpods` namespace: quota on pods, requests/limits CPU & memory, PVC count and storage; LimitRange default/max per container matching catalog max.

**AC**

- A pod spec without resources gets defaults; a request above max is rejected by the admission controller (test by `kubectl apply`).

### DP-8.2 Admission policies (Kyverno or Gatekeeper / ValidatingAdmissionPolicy)

- Policies for `devpods` namespace: require labels `devpods.io/owner`, disallow `hostPath`/`hostNetwork`/privileged, require image from `registry.example.com/devpods/*` and signed (cosign verify policy), disallow `latest`, require `runAsNonRoot`.

**AC**

- Each policy has a passing and a failing test manifest in CI (`kyverno test` or `gator test`); API-created pods pass all policies.

### DP-8.3 NetworkPolicies for dev pods

- Default deny ingress/egress in `devpods`; allow egress DNS, internet (or proxy), Git, registry; allow ingress 2222 only from bastion; pod-to-pod between different owners denied.

**AC**

- Two pods from different users cannot reach each other on any port (verified with `nc`); same-user pods behaviour as decided in ADR.

### DP-8.4 Secrets handling

- Per-pod Secret for authorized keys is owned (ownerReference) by the pod/StatefulSet so it is garbage-collected; API DB credentials via External Secrets; no secrets in logs (log masking test).

**AC**

- Deleting a pod removes its Secret; grep of API logs in integration tests finds no key material or tokens.

### DP-8.5 API security hardening

- Rate limiting (Bucket4j or Istio local rate limit) on `POST /pods`; input size limits; CORS locked; security headers; dependency scanning (OWASP dependency-check / Snyk) in CI; SAST (CodeQL/Semgrep).

**AC**

- 20 create requests in 10 s from one user return 429 after the limit; CI fails on HIGH dependency CVE with fix; OWASP ZAP baseline scan against dev shows no High alerts.

### DP-8.6 Threat model and security review

- STRIDE session covering: token theft, pod escape, lateral movement, SSH key injection, quota abuse, supply chain; document mitigations and residual risks.

**AC**

- Threat model document approved by security; every High risk has a linked story or accepted-risk sign-off.

### DP-8.7 Pod runtime security (optional, stretch)

- Falco or Tetragon rules for dev pods (shell from sshd is expected; alert on privilege escalation attempts, kube-apiserver access, crypto-mining patterns).

**AC**

- Test rule fires in dev when a pod attempts to reach the kube-apiserver; alert reaches the ops channel.

## Epic 9 — Observability, operations and lifecycle

Goal: the team can see pod counts, failures and cost; idle pods stop themselves; the platform can be upgraded and restored.

### DP-9.1 Metrics, dashboards and alerts

- Prometheus `ServiceMonitor` for API and bastion; Grafana dashboard: pods by status, creates/day, create latency p95, failures by reason, CPU/mem per user, quota utilisation; alerts: API down, reconciler lease lost, create failure rate > 10% over 15 min, namespace quota > 90%.

**AC**

- Dashboard JSON committed in `devpods-helm/dashboards`; each alert has a runbook link and fires in a dev test.

### DP-9.2 Centralised logs and tracing

- API/portal/bastion logs shipped to the org log stack (Loki/ELK) with `traceId`; traces to Tempo/Jaeger; bastion auth logs (who connected to which pod) retained 90 days.

**AC**

- A failed create can be traced from the portal error toast (shows `traceId`) to the K8s error in logs.

### DP-9.3 Idle detection and auto-stop

- Signal: last SSH session end (bastion log / `who` in pod via a tiny agent reporting to API `POST /internal/heartbeat`) and pod CPU below threshold; default idle timeout 2 h (user-configurable up to admin max); warning email/portal banner 15 min before stop.

**AC**

- A pod with no SSH session and < 50 mCPU for 2 h transitions to `STOPPED`; `last_activity_at` updates on SSH login; user can set timeout 30 min–8 h.

### DP-9.4 TTL and garbage collection

- Optional hard TTL per pod (default 7 days, admin max 30); `STOPPED` pods deleted after 14 days with 3-day warning; orphan K8s resources without DB record cleaned after 1 h.

**AC**

- Scheduled job logs each deletion to `audit_event`; a pod created with `ttlHours=1` is deleted at T+1h ± 5 min.

### DP-9.5 Backup and restore

- Postgres backups (daily, 30-day retention) and restore runbook; PVC snapshots for persistent workspaces (Phase 2) via CSI snapshots or Velero.

**AC**

- Restore drill on dev restores the DB to a point in time and the API reconciles the existing pods correctly.

### DP-9.6 Runbooks and on-call

- Runbooks: API down, Keycloak unreachable, pods stuck `PROVISIONING`, image pull failures, bastion full, quota exhausted, cert expiry.

**AC**

- Each alert in DP-9.1 links to a runbook; runbooks reviewed by on-call engineers.

### DP-9.7 Cost reporting

- Label-based cost allocation (OpenCost/Kubecost) by `devpods.io/owner`; monthly report per team.

**AC**

- Report shows cost per user for the last month; top 10 users visible to admins in the portal (Phase 2 UI).

## Epic 10 — Testing & QA

Goal: every layer has automated tests that run in CI, plus one end-to-end journey that proves a developer can log in, create a pod and SSH in.

### DP-10.1 Unit test standards and coverage gates

- API: JUnit 5 + Mockito, JaCoCo ≥ 80% domain, ≥ 60% overall; Portal: Vitest + Testing Library, ≥ 70% components; Charts: helm-unittest for every template.

**AC**

- Coverage gates block merges; reports published in CI.

### DP-10.2 API integration tests (Testcontainers + kind)

- Postgres via Testcontainers; Keycloak via Testcontainers (realm import with test users `alice` user, `bob` user, `admin`); K8s via kind in CI or fabric8 mock server.
- Scenarios: create → running, stop/start, delete, quota exceeded, forbidden cross-user access, reconciler drift.

**AC**

- Suite runs in CI under 10 minutes; all scenarios green; flaky test rate < 2% over 20 runs.

### DP-10.3 Contract tests between portal and API

- OpenAPI-driven contract tests (Pact or schemathesis) so API changes that break the portal fail in the API pipeline.

**AC**

- Removing a response field used by the portal fails the API build.

### DP-10.4 Image tests

- `contract-test.sh` per image (DP-2.2); tool smoke tests (`java -version`, `mvn package` sample); structure tests (container-structure-test) for users, ports, files.

**AC**

- Every image build runs its tests; a failing tool test blocks the `stable` tag promotion.

### DP-10.5 Helm chart install tests

- `ct install` on kind for both charts with default and prod-like values; upgrade test from previous released version; `helm diff` review in PRs.

**AC**

- Fresh install and upgrade succeed in CI; `NOTES.txt` prints the portal URL.

### DP-10.6 End-to-end test (Playwright + SSH)

- Against the dev cluster: log in as `alice`, add SSH key (test key), create Java pod, wait `RUNNING`, SSH via bastion, run `java -version`, stop, start, delete; verify `bob` cannot see Alice's pod.

**AC**

- E2E runs nightly and on release candidates; total time < 15 minutes; failures post to the team channel with trace link.

### DP-10.7 Load and capacity test

- k6/Gatling: 50 concurrent users creating pods, 200 pods steady state; measure API p95, reconciler lag, bastion connection limit.

**AC**

- API p95 < 500 ms for list endpoints at 200 pods; create-to-running p95 < 60 s (warm image); documented capacity limits and node sizing.

### DP-10.8 Security testing

- OWASP ZAP baseline (API + portal), `kube-bench`/`kube-hunter` on cluster, policy tests (DP-8.2), SSH hardening scan (`ssh-audit`) on bastion and a dev pod.

**AC**

- No High findings open at release; `ssh-audit` grade A on bastion.

### DP-10.9 Chaos and resilience checks

- Kill API pod during create; restart Keycloak; delete a dev pod manually; node drain with running pods.

**AC**

- No pod left in an inconsistent state after each scenario; reconciler corrects within 60 s; documented behaviour for node drain (pods are recreated, data preserved when PVC).

## Epic 11 — MVP release & documentation

Goal: a pilot group of developers uses the platform daily with one Java image; docs let a new user connect in under 10 minutes without help.

### DP-11.1 User documentation

- `devpods-docs`: Getting started (login, add SSH key, create pod, connect with VS Code), SSH config snippet, FAQ (data loss on stop, idle timeout, quotas), troubleshooting (key refused, pod `FAILED` reasons).

**AC**

- A developer outside the team follows the guide and reaches a shell in their pod in under 10 minutes; feedback incorporated.

### DP-11.2 Operator documentation

- Install/upgrade with Helm and Argo CD, values reference (helm-docs), Keycloak client setup, adding a new image to the catalog, runbooks index, architecture and ADR links.

**AC**

- A platform engineer installs the platform on a fresh kind cluster from the docs only.

### DP-11.3 Release process and versioning

- Semantic versioning per component; release notes generated; compatibility matrix (API ↔ chart ↔ portal); `CHANGELOG.md`.

**AC**

- `v0.1.0` tags for images, API, portal and charts exist with release notes; chart `appVersion` matches images.

### DP-11.4 Pilot rollout

- Pick 5–10 pilot developers; enable `devpods-user` role for their group; collect feedback for 2 weeks; track create success rate and SSH success rate.

**AC**

- ≥ 90% create success, ≥ 95% SSH success during pilot; feedback triaged into Phase 2 backlog; go/no-go decision recorded.

### DP-11.5 MVP acceptance checklist

- [ ] Login via Keycloak with roles enforced (DP-3.x, DP-5.2)
- [ ] Catalog with `base` and `java` images (DP-2.x, DP-5.4)
- [ ] Create, list, stop, start, delete own pods (DP-4.6, DP-5.5–5.6)
- [ ] SSH from laptop via bastion and VS Code Remote (DP-7.2, DP-7.5)
- [ ] Quotas enforced (DP-4.9, DP-8.1)
- [ ] Idle auto-stop (DP-9.3)
- [ ] Helm install/upgrade via GitOps (DP-6.x)
- [ ] Dashboards and alerts live (DP-9.1)
- [ ] Security review passed (DP-8.6, DP-10.8)
- [ ] E2E suite green on release candidate (DP-10.6)

## Epic 12 — Phase 2: additional tool images and catalog choices

Goal: developers pick from a broad, maintained catalog; each image is one story with the same template so new images are cheap to add.

### Story template for every new image (copy per image)

As a `<language>` developer, I want a `devpods/<tool>` image so that I can start work without installing tools.

- `FROM devpods/base:stable`; install tools listed; version pinned; non-root; README with tool versions; add `catalog.yaml` entry with icon, tags and default resources; smoke test script.

**AC (common)**

- Passes `contract-test.sh`, Trivy gate, structure test and tool smoke test; appears in the portal catalog; a pod created from it reaches `RUNNING` in under 90 s warm; documented in `devpods-docs/images/<tool>.md`.

### Image stories

| Story | Image | Contents | Default CPU/Mem | Notes |
| --- | --- | --- | --- | --- |
| DP-12.1 | `devpods/node` | Node 20 & 22 via nvm, npm, pnpm, yarn, corepack | 1 / 2 GiB |  |
| DP-12.2 | `devpods/python` | Python 3.11/3.12, uv, pipx, poetry, venv | 1 / 2 GiB |  |
| DP-12.3 | `devpods/go` | Go 1.23, golangci-lint, delve | 1 / 2 GiB |  |
| DP-12.4 | `devpods/dotnet` | .NET 8 SDK | 2 / 4 GiB |  |
| DP-12.5 | `devpods/fullstack` | java + node + python | 2 / 4 GiB | Larger image; measure size |
| DP-12.6 | `devpods/data` | Python + Jupyter + pandas + DuckDB | 2 / 8 GiB | Preview URL for Jupyter (DP-7.7) |
| DP-12.7 | `devpods/cloud-tools` | kubectl, helm, terraform, aws/az/gcloud CLIs | 1 / 2 GiB | No cluster creds by default |
| DP-12.8 | `devpods/rust` | rustup stable, cargo, clippy | 2 / 4 GiB |  |
| DP-12.9 | `devpods/android` | JDK, Android SDK cmdline tools | 4 / 8 GiB | Large; optional |
| DP-12.10 | `devpods/docker-in-pod` | Base + rootless Docker/Podman or Kaniko/BuildKit | 2 / 4 GiB | Security review required; prefer BuildKit rootless |

### DP-12.11 Catalog features: tags, categories, versions

- Catalog supports multiple versions per image (`java:17`, `java:21`), categories, “recommended” flag, per-image max resources, deprecation date.

**AC**

- Portal groups by category and shows version dropdown; deprecated images are hidden from create but existing pods still run.

### DP-12.12 Self-service custom image requests

- Form in portal creates a Jira/GitHub issue with the requested tools; admins can add `extraPackages` to an image via catalog override without a rebuild (installed by init hook at start, with a warning about start time).

**AC**

- Request creates a ticket with the template filled; `extraPackages: ["httpie"]` on a catalog entry results in `http --version` working in a new pod.

### DP-12.13 Image build matrix and cache optimisation

- Shared base layers, BuildKit cache mounts, parallel matrix builds; weekly rebuild extended to all images; size budget per image enforced in CI.

**AC**

- Full matrix rebuild under 30 minutes; CI fails if an image exceeds its size budget.

## Epic 13 — Phase 2: advanced features

Goal: move from “disposable pod” to a daily workspace: persistent home, templates, repo bootstrap, browser IDE, sharing and cost visibility. Prioritise from pilot feedback (DP-11.4).

### DP-13.1 Persistent home volumes

- PVC per pod (default on), size selectable (10/20/50 GB), `fsGroup` 1000, resize support, snapshot before delete with 7-day restore window.

**AC**

- Files in `/home/dev` survive stop/start and image version change; delete shows “restore within 7 days” and restore works.

### DP-13.2 Dotfiles and repo bootstrap

- User profile: dotfiles repo URL (cloned at start), default Git repos to clone, Git identity; Git credentials via short-lived token from Keycloak-federated provider or user-provided PAT stored as K8s Secret.

**AC**

- New pod has dotfiles applied and repo cloned before SSH is accepted (or shows “bootstrapping” status); PAT never appears in logs or pod env listing (mounted as file).

### DP-13.3 Workspace templates

- Admin-defined templates: image + resources + repos + env vars + init script + exposed ports; users create from template in one click; team-scoped templates by Keycloak group.

**AC**

- Template “Payments backend” creates a pod with the right image, repos and `make setup` run; visible only to the `payments` group.

### DP-13.4 Browser IDE (code-server / VS Code Web) via Istio

- Optional sidecar/container running code-server on 8443 behind Istio with JWT `AuthorizationPolicy` restricted to the owner; portal “Open in browser” button.

**AC**

- Owner opens IDE after SSO without a password; another user gets 403; works on a locked-down laptop with no SSH client.

### DP-13.5 Pod sharing and pair programming

- Owner grants read/SSH access to named users for a time window; audit logged; revoke any time.

**AC**

- Shared user can SSH until expiry, then is refused; owner sees active shares.

### DP-13.6 Scheduling: auto-start and working hours

- Per-pod schedule (e.g. start 08:00 Mon–Fri, stop 19:00) and “wake on SSH” via bastion triggering start and holding the connection.

**AC**

- SSH to a stopped pod starts it and connects within 90 s; schedule stops it at 19:00 local time.

### DP-13.7 Resource presets and GPU pods

- Presets S/M/L/XL; GPU node pool with `nvidia.com/gpu` requests for ML images; separate quota; cost warning.

**AC**

- GPU pod shows `nvidia-smi` output; non-GPU users cannot select GPU preset.

### DP-13.8 Notifications

- Email/Slack/Teams on: pod ready, idle stop warning, TTL deletion warning, failed create. User preferences page.

**AC**

- Pilot users receive “pod ready” within 30 s of `RUNNING`; opt-out honoured.

### DP-13.9 Cost dashboard in portal

- Per-user and per-team monthly cost from DP-9.7; budget alerts.

**AC**

- Admin sees top users and trend; user sees own month-to-date cost on My pods page.

### DP-13.10 CLI (`devpod` command)

- Go/Java CLI using device-code OIDC login: `devpod list|create|ssh|stop|delete`; `devpod ssh <name>` wraps the ProxyJump.

**AC**

- `devpod create --image java my-pod && devpod ssh my-pod` works end to end from a laptop; published as a release binary for macOS/Linux/Windows.

### DP-13.11 Multi-cluster / region support

- API manages pods in several clusters (kubeconfig per cluster, region selection in UI); bastion per cluster.

**AC**

- A pod created with `region=eu` lands in the EU cluster and the SSH snippet targets the EU bastion.

### DP-13.12 Kubernetes-native model: `DevPod` CRD and operator (refactor option)

- Replace direct pod creation with a `DevPod` CRD reconciled by an operator (Java Operator SDK or Go); API becomes a thin client; enables GitOps of workspaces and simpler drift handling.

**AC**

- ADR accepted; `kubectl get devpods` lists workspaces with status; API behaviour unchanged for the portal (contract tests pass).

## Suggested sprint ordering and Definition of Done

Two-week sprints; a team of 2 backend, 1 frontend, 2 platform/SRE engineers reaches a pilot in roughly 6 sprints. Order follows dependencies: images before Helm, API before portal, SSH path before pilot.

| Sprint | Focus | Stories |
| --- | --- | --- |
| 1 | Foundations + base image | DP-1.1–1.6, DP-2.1, DP-2.2, DP-3.1 |
| 2 | Java image, Keycloak client, API skeleton | DP-2.3–2.6, DP-3.2, DP-3.3, DP-4.1–4.4 |
| 3 | API pod lifecycle, dev-pod chart | DP-4.5–4.8, DP-6.1, DP-6.7, DP-10.1, DP-10.2 |
| 4 | Portal MVP, platform chart, Istio HTTP | DP-5.1–5.7, DP-6.2–6.6, DP-6.9 |
| 5 | SSH path, security, quotas | DP-3.5, DP-4.9, DP-6.8, DP-7.1–7.6, DP-8.1–8.5 |
| 6 | Ops, tests, docs, pilot | DP-4.10–4.11, DP-5.8–5.10, DP-9.1–9.4, DP-10.3–10.8, DP-11.1–11.5 |
| 7+ | Phase 2 | DP-12.x then DP-13.x by pilot feedback |

### Definition of Done (applies to every story)

- Code merged to `main` via reviewed PR; CI green (build, unit tests, lint, scan).
- Acceptance criteria demonstrated in the dev environment and ticked in the story.
- Security context, resources and labels follow the conventions in the Overview.
- Docs updated (README, helm-docs, user or operator docs) in the same PR.
- Observability: new endpoints/jobs emit metrics and structured logs.
- No new HIGH/CRITICAL vulnerabilities with a fix available.
- Feature flags or values defaults chosen so that an upgrade does not break existing pods.

### How to load into Jira

- Create the 13 Epics from the Epic map; create each `DP-x.y` as a Story under its epic; paste the task bullets into the description and the AC bullets into the Acceptance Criteria field (or a checklist).
- Add labels `phase-1`/`phase-2` and component labels `images`, `api`, `portal`, `helm`, `istio`, `security`, `ops`, `docs`.
- Link dependencies with “blocks/is blocked by” using the Epic map's Depends on column.
- A CSV import (Summary, Description, Epic Link, Labels) can be generated from this doc on request.
