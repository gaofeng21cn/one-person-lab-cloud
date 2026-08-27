import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeWorkspaceImageReleaseCatalog,
  type WorkspaceImageReleaseCatalog
} from "../../packages/contracts/workspace-image-release.ts";

const installedImage = `registry.example.com/opl/workspace@sha256:${"b".repeat(64)}`;
const rollbackImage = `registry.example.com/opl/workspace@sha256:${"a".repeat(64)}`;

test("Workspace image release catalog accepts unique immutable versions containing the installed image", () => {
  const input: WorkspaceImageReleaseCatalog = {
    schemaVersion: 1,
    releases: [
      { version: "26.8.26", image: installedImage },
      { version: "26.8.4", image: rollbackImage }
    ]
  };
  assert.deepEqual(decodeWorkspaceImageReleaseCatalog(JSON.stringify(input), installedImage), input);
});

test("Workspace image release catalog rejects mutable, duplicate, unknown, and incomplete inputs", () => {
  for (const input of [
    { schemaVersion: 1, releases: [{ version: "latest", image: "registry.example.com/opl/workspace:latest" }] },
    { schemaVersion: 1, releases: [{ version: "same", image: installedImage }, { version: "same", image: rollbackImage }] },
    { schemaVersion: 1, releases: [{ version: "26.8.4", image: rollbackImage }] },
    { schemaVersion: 1, releases: [{ version: "26.8.26", image: installedImage, mutable: true }] }
  ]) {
    assert.equal(decodeWorkspaceImageReleaseCatalog(JSON.stringify(input), installedImage), null);
  }
});
