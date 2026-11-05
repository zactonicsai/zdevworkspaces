# Static JSON fixtures

Response examples for the Workspaces API (shapes from `api/openapi.yaml`), used in two places:

* the **UI stub switch** (`?stub=1`, the footer toggle, `PORTAL_CONFIG.stub`, or any `file://`
  open): `stub.js` loads these and answers every call in the browser, keeping small in-memory
  changes (create, start/stop, drives…) so the UI can be developed with no API at all;
* the **Python API's `--stub` flag** (`portal-py/api.py --stub`): the server answers from the
  same files, so the UI, `wsctl` and curl can be tested against a running HTTP API without a
  cluster.

Edit freely; `workspaces.json`, `drives.json`, `snapshots.json` seed the state, the rest are
returned as-is.
