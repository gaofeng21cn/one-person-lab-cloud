import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

// The product declares its own migration journal.
//
// Every owner process records the migrations it installed in the shared
// `opl_schema_migrations` journal, under the product's own service label, with
// the migration file's stem as the version
// (`services/internal/postgresmigrate/migrate.go:19-23`,
// `services/internal/postgresmigrate/migrate.go:264-266`,
// `services/internal/ownerstore/store.go:127-128`). An installation that
// restores a database has to know which versions the target binary would
// install, and only the product knows that: the version set has three different
// shapes (a hand-written Go list, an embedded `ent_migrations` directory, and an
// owner-local `migrations` directory), and the migration SQL's database guard is
// not universal, so no reader of the source tree can derive the set.
//
// So the product publishes the set and this test proves the declaration still
// matches the code that writes the journal. The declaration is the product's
// answer; a guard that re-derives it from the same source would only prove the
// declaration can be computed, which is the thing that stopped working.

const contractPath = "packages/contracts/opl-cloud-migration-journal-contract.json";
const ownerIdentityPath = "packages/contracts/go/owneridentity/owneridentity.go";

type Source = { kind: string; path?: string; file?: string; symbol?: string };
type Journal = { service: string; source: Source; versions: string[] };
type Contract = { schemaVersion: number; journals: Journal[] };

async function loadContract(): Promise<Contract> {
  return JSON.parse(await readFile(contractPath, "utf8")) as Contract;
}

// sqlStems reads the migration file stems of one directory in the product's own
// order: the whole stem compared as a string, which is what
// `ownerstore.ReadMigrations` sorts by (`services/internal/ownerstore/store.go:132`).
async function sqlStems(directory: string): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true });
  return entries
    .filter((entry) => entry.isFile() && entry.name.endsWith(".sql"))
    .map((entry) => entry.name.replace(/\.sql$/, ""))
    .sort();
}

// goFiles lists the product's own Go sources, excluding tests: a test may open
// a store, but only production code installs a migration.
async function goFiles(directory: string): Promise<string[]> {
  const found: string[] = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) found.push(...(await goFiles(full)));
    else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) found.push(full);
  }
  return found;
}

// declaredServiceLabels derives every label the product writes into a journal
// from the production sources, so the declaration cannot omit an owner and call
// itself complete.
//
// Three writes exist. Two name their label literally:
// `postgresmigrate.Apply(ctx, db, "<label>", ...)` for the retained shared
// database and `ownerstore.New(db, "<label>")` for an owner-local store. The
// third names it indirectly: the ownerservice path opens the store with
// `ownerstore.New(db, owner.String())`
// (`services/internal/ownerservice/database.go:52`), so its labels are the
// canonical owner names in the shared owner identity
// (`packages/contracts/go/owneridentity/owneridentity.go`).
async function declaredServiceLabels(): Promise<Set<string>> {
  const labels = new Set<string>();
  const ownerIdentity = await readFile(ownerIdentityPath, "utf8");
  for (const match of ownerIdentity.matchAll(/^\t(\w+)\s+Owner = "([a-z_]+)"$/gm)) labels.add(match[2]);

  for (const file of await goFiles("services")) {
    const source = await readFile(file, "utf8");
    for (const match of source.matchAll(/postgresmigrate\.Apply\([^,]+,\s*[^,]+,\s*"([a-z0-9_-]+)"/g)) labels.add(match[1]);
    for (const match of source.matchAll(/ownerstore\.New\([^,]+,\s*"([a-z0-9_-]+)"/g)) labels.add(match[1]);
  }
  return labels;
}

test("the declaration carries the shape an installation reads", async () => {
  const contract = await loadContract();
  assert.equal(contract.schemaVersion, 1);
  assert.ok(Array.isArray(contract.journals) && contract.journals.length > 0);
  for (const journal of contract.journals) {
    assert.deepEqual(Object.keys(journal).sort(), ["service", "source", "versions"]);
    assert.equal(typeof journal.service, "string");
    assert.ok(journal.service.length > 0, "every journal entry names the product's own service label");
    assert.ok(Array.isArray(journal.versions) && journal.versions.length > 0,
      `${journal.service} declares the versions it installs`);
    assert.equal(new Set(journal.versions).size, journal.versions.length,
      `${journal.service} declares no version twice`);
    for (const version of journal.versions) {
      assert.equal(typeof version, "string");
      assert.ok(version.length > 0, `${journal.service} declares no empty version`);
    }
    if (journal.source.kind === "sqlDirectory") {
      assert.deepEqual(Object.keys(journal.source).sort(), ["kind", "path"]);
      assert.equal(typeof journal.source.path, "string");
    } else if (journal.source.kind === "goMigrationList") {
      assert.deepEqual(Object.keys(journal.source).sort(), ["file", "kind", "symbol"]);
      assert.equal(typeof journal.source.file, "string");
      assert.equal(typeof journal.source.symbol, "string");
    } else {
      assert.fail(`unknown source kind ${journal.source.kind}`);
    }
  }
});

test("every declared service label is one the product writes into a journal", async () => {
  const contract = await loadContract();
  const declared = new Set(contract.journals.map((journal) => journal.service));
  const written = await declaredServiceLabels();

  assert.deepEqual([...declared].sort(), [...written].sort(),
    "the declaration names exactly the service labels the product writes into a migration journal");
  for (const service of declared) {
    assert.ok(written.has(service), `${service} is declared but no product source writes that label`);
  }
});

test("every declared sql directory holds exactly the versions it declares", async () => {
  const contract = await loadContract();
  const directories = contract.journals.filter((journal) => journal.source.kind === "sqlDirectory");
  assert.ok(directories.length >= 8, `expected the owner migration directories, saw ${directories.length}`);

  for (const journal of directories) {
    const directory = journal.source.path as string;
    let stems: string[];
    try {
      stems = await sqlStems(directory);
    } catch (error) {
      assert.fail(`${journal.service} declares ${directory}, which the product does not have: ${String(error)}`);
    }
    assert.deepEqual(journal.versions, stems,
      `${journal.service} declares versions that are not the migration files in ${directory}`);
  }
});

test("the fabric journal declares both migrations the product installs under that label", async () => {
  const contract = await loadContract();
  const fabric = contract.journals.filter((journal) => journal.service === "fabric");
  assert.equal(fabric.length, 2,
    "the retained operation store and the owner surface both install migrations under the fabric label");

  const retained = fabric.filter((journal) => journal.source.path === "services/fabric/internal/fabric/ent_migrations");
  const owner = fabric.filter((journal) => journal.source.path === "services/fabric/ownermigrations");
  assert.equal(retained.length, 1, "the retained operation store's migrations are declared");
  assert.equal(owner.length, 1, "the owner surface's own migrations are declared");
  assert.ok(owner[0].versions.includes("0001_opl_fabric"),
    "the owner surface installs 0001_opl_fabric under the fabric label");
});
