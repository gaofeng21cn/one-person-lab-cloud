# Agent Workbench WebUI

A standalone React/assistant-ui frontend extracted from the original IBD three-panel application. It preserves the conversation history, chat, reference panel and responsive drawers. Original source provenance is retained in `SOURCE-PROVENANCE.json`; `IMPORT-PROVENANCE.json` records every extracted source file hash and the import adaptations. React source files are imported byte for byte.

## Ownership and scope

This package contains frontend code only. It does not run an Agent, own a model key, query a knowledge database, provision a Workspace, or replace a Runtime. The original IBD FastAPI/RAGFlow backend is not included. The implemented adapter explicitly targets the OPL native `g2` / `opl_app_state.v1` directory and canonical thread HTTP protocol. Other Agents using that same protocol use the same UI; a different backend protocol needs its own explicit adapter and tests. No protocol is guessed.

## Build and verify

Use Node 24 and npm. Browser checks use installed Google Chrome by default. CI explicitly selects Playwright Chromium; use the same browser locally with `npx playwright install chromium` and `WEBUI_BROWSER_CHANNEL=chromium npm run test:browser`. A missing selected browser fails the test.

```sh
npm ci
npm test
npm run build
npm run test:browser
```

Unit and HTTP-adapter tests use fixtures. Browser regression uses a labelled local fixture backend with two nonmedical Agents. They do not establish a real deployment. `tests/live-runtime.mjs` is a manual replay utility; no live Runtime receipt is included or established by this source import.

## Presentation configuration

Edit `public/webui-config.json` before building:

```json
{
  "schemaVersion": 1,
  "runtimeProtocol": "opl-native-g2",
  "sidebarTitle": "Agent Workbench",
  "assistantTitle": null,
  "notice": null
}
```

A null assistant title uses the selected Agent display name. A notice is optional `{ "title": "...", "body": "..." }`; no medical warning is built in. Agent choices come from the authenticated Runtime directory. Changing the selected Agent starts a new conversation. An existing conversation keeps its Runtime-owned binding. Session storage holds only navigation/choice identifiers, never credentials.

## Runtime integration

Serve `dist/` from the same origin as the Runtime. Native password login remains owned by that Runtime. The UI reads `/api/auth/user` for session/CSRF and `/api/opl/state?profile=fast` for executable Agent declarations. Native thread list/read/rename/delete and send endpoints remain the only business API. Every POST carries the session CSRF token and uses `redirect: error`.

The first send includes the exact directory-provided Agent selection; continuation sends its canonical thread ID. Success requires a matching completed canonical turn and exact native answer text. Runtime SSE events trigger authoritative thread readback rather than being treated as completed answers. Text input is supported. Server cancellation is not exposed by this frontend version; aborting a browser request is not presented as cancelling an Agent.

References use a domain-neutral presentation type and preserve supplied titles. The current native adapter has no structured reference projection, so it returns an empty evidence list and the panel says so. It never manufactures citations by parsing generated text. Structured citations from the original IBD backend are not claimed to work through this different protocol.

## OCI artifact

```sh
npm ci
npm run build
docker buildx build --platform linux/amd64 --network none \
  --output type=oci,dest=agent-webui.oci.tar \
  --metadata-file webui-build-metadata.json .
```

The scratch OCI contains only compiled static files under `/web`. It is an immutable WebUI input consumed by the existing application Build; it is not an independently executable application container. The Runtime supplies HTTP serving, authentication and port/health behavior. Publishing the WebUI to a Registry, catalog admission, composing a final Agent OCI and Tencent delivery are subsequent owner operations, not results of this local extraction.
