import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { buildTencentProvisioner } from "../../tools/build-tencent-provisioner.ts";

const execute = promisify(execFile);

test("Tencent provisioner build stamps the exact checkout revision", async () => {
  const directory = await mkdtemp(join(tmpdir(), "opl-tencent-provisioner-build-test-"));
  const binDirectory = join(directory, "bin");
  const argsPath = join(directory, "go-args.log");
  const outputPath = join(directory, "provisioner");
  await mkdir(binDirectory);
  await writeFile(join(binDirectory, "go"), `#!/bin/sh
printf '%s\\n' "$*" > "${argsPath}"
exit 0
`);
  await chmod(join(binDirectory, "go"), 0o700);
  const sourceSHA = (await execute("git", ["rev-parse", "HEAD"])).stdout.trim();
  const previousPath = process.env.PATH;
  const previousRelease = process.env.OPL_RELEASE_SHA;
  process.env.PATH = `${binDirectory}:${previousPath || ""}`;
  process.env.OPL_RELEASE_SHA = sourceSHA;
  try {
    assert.equal(await buildTencentProvisioner({ binPath: outputPath }), outputPath);
    const args = await readFile(argsPath, "utf8");
    assert.match(args, /build/);
    assert.match(args, /-ldflags/);
    assert.match(args, new RegExp(`ProductSourceSHA=${sourceSHA}`));
  } finally {
    if (previousPath === undefined) delete process.env.PATH;
    else process.env.PATH = previousPath;
    if (previousRelease === undefined) delete process.env.OPL_RELEASE_SHA;
    else process.env.OPL_RELEASE_SHA = previousRelease;
    await rm(directory, { recursive: true, force: true });
  }
});

test("Tencent provisioner build refuses a release claim from another revision", async () => {
  const directory = await mkdtemp(join(tmpdir(), "opl-tencent-provisioner-mismatch-test-"));
  const binDirectory = join(directory, "bin");
  await mkdir(binDirectory);
  await writeFile(join(binDirectory, "go"), "#!/bin/sh\nexit 0\n");
  await chmod(join(binDirectory, "go"), 0o700);
  const previousRelease = process.env.OPL_RELEASE_SHA;
  const previousPath = process.env.PATH;
  process.env.OPL_RELEASE_SHA = "b".repeat(40);
  process.env.PATH = `${binDirectory}:${previousPath || ""}`;
  try {
    await assert.rejects(
      buildTencentProvisioner({ binPath: join(tmpdir(), "opl-tencent-provisioner-mismatch") }),
      /OPL_RELEASE_SHA does not match the checkout revision/
    );
  } finally {
    if (previousRelease === undefined) delete process.env.OPL_RELEASE_SHA;
    else process.env.OPL_RELEASE_SHA = previousRelease;
    if (previousPath === undefined) delete process.env.PATH;
    else process.env.PATH = previousPath;
    await rm(directory, { recursive: true, force: true });
  }
});
