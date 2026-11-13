import { mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));

export const defaultProvisionerBin = join(root, ".runtime", "opl-tencent-provisioner");

function run(command, args, cwd) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd, stdio: "pipe" });
    let stderr = "";
    child.stderr.on("data", (chunk) => {
      stderr += chunk.toString();
    });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code === 0) resolve();
      else reject(new Error(`${command} ${args.join(" ")} failed:${stderr.trim()}`));
    });
  });
}

function runCapture(command, args, cwd) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => { stdout += chunk.toString(); });
    child.stderr.on("data", (chunk) => { stderr += chunk.toString(); });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code === 0) resolve(stdout.trim());
      else reject(new Error(`${command} ${args.join(" ")} failed:${stderr.trim()}`));
    });
  });
}

export async function buildTencentProvisioner({ binPath = defaultProvisionerBin } = {}) {
  const sourceSHA = await runCapture("git", ["rev-parse", "HEAD"], root);
  if (!/^[0-9a-f]{40}$/.test(sourceSHA)) {
    throw new Error("checkout source revision is not an exact commit SHA");
  }
  const claimedSHA = String(process.env.OPL_RELEASE_SHA || "").trim();
  if (claimedSHA && claimedSHA !== sourceSHA) {
    throw new Error("OPL_RELEASE_SHA does not match the checkout revision");
  }
  await mkdir(dirname(binPath), { recursive: true });
  await run("go", [
    "build", "-ldflags", `-X opl-cloud/packages/contracts/go.ProductSourceSHA=${sourceSHA}`,
    "-o", binPath, "./cmd/opl-tencent-provisioner"
  ], join(root, "services", "fabric"));
  return binPath;
}
