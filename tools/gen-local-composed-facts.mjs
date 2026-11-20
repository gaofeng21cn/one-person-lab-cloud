// Generate the local installation facts for the composed topology.
//
// The facts generator is parameterised by the Compose file, so the composed
// process space reuses every fact the split form declares: the same peer
// tokens, the same databases, the same registry binding.
import { createHash } from "node:crypto";
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { cleanHostInstallation } from "./local-v2-clean-host.ts";
import { localV2EnvFile } from "./local-v2-installation-facts.ts";

const directory = resolve(process.argv[2] || "/tmp/opl-local-composed");
const cloudImage = process.argv[3];
const workspaceImage = process.argv[4];
const sourceSha = process.argv[5];

mkdirSync(resolve(directory, "pki"), { recursive: true });
mkdirSync(resolve(directory, "fabric-secrets"), { recursive: true });
mkdirSync(resolve(directory, "storage"), { recursive: true });
const schema = resolve(directory, "plugin.schema.json");
copyFileSync(resolve("deploy/portable/agent-plugins-1.0.0-plugin.schema.json"), schema);
const publisherSchemaDigest = `sha256:${createHash("sha256").update(readFileSync(resolve("docs/spec/target/contracts/publisher-contract.schema.json"))).digest("hex")}`;

const compose = readFileSync(resolve("deploy/portable/compose.local-composed.yaml"), "utf8");
const template = readFileSync(resolve("deploy/portable/opl-cloud.env.example"), "utf8");

const installation = cleanHostInstallation({
  template,
  compose,
  directory,
  sourceSha,
  cloudImage,
  workspaceImage,
  storageRoot: resolve(directory, "storage"),
  project: "opl-local-composed",
  publisherSchemaDigest,
});

writeFileSync(resolve(directory, "installation.env"), localV2EnvFile(installation), { mode: 0o600 });
console.log("facts written:", resolve(directory, "installation.env"));
