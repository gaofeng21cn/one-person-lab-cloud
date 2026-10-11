import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import test from "node:test";

const require = createRequire(import.meta.url);
const { SourceMapConsumer } = require("source-map-js");
const postcss = require("postcss");
const flatMap = { version: 3, sources: ["input.css"], sourcesContent: ["a {}"], names: [], mappings: "AAAA" };
const section = (line: unknown, column: unknown, map: unknown = flatMap) => ({ version: 3, sections: [{ offset: { line, column }, map }] });

test("source-map decoding rejects offsets that can amplify tiny indexed maps", () => {
  for (const value of [-1, 1.5, Infinity, NaN, "1", null]) {
    assert.throws(() => new SourceMapConsumer(section(value, 0)));
    assert.throws(() => new SourceMapConsumer(section(0, value)));
  }
  assert.throws(() => new SourceMapConsumer(section(10_000_001, 0)), /must not exceed/);
  const nested = section(5_000_000, 0, section(5_000_000, 0, section(5_000_000, 0)));
  assert.throws(() => new SourceMapConsumer(nested), /nested sections/);
});

test("deep indexed maps complete within a bounded process instead of blocking the build", () => {
  // The vulnerable getter grows exponentially with nesting. Keep the probe in
  // a bounded child so a dependency regression cannot hang the test runner.
  const script = `
    const { SourceMapConsumer, SourceNode } = require('source-map-js');
    let map = ${JSON.stringify(flatMap)};
    for (let i = 0; i < 40; i++) map = { version: 3, sections: [{ offset: { line: 0, column: 0 }, map }] };
    const node = SourceNode.fromStringWithSourceMap('a {}', new SourceMapConsumer(map));
    process.stdout.write(node.toString());
  `;
  const result = spawnSync(process.execPath, ["--max-old-space-size=128", "-e", script], {
    cwd: process.cwd(), encoding: "utf8", timeout: 2_000, maxBuffer: 64 * 1024
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, "a {}");
});

test("the PostCSS consumer still emits readable source maps for valid CSS", async () => {
  const css = "a { color: red }";
  const result = await postcss([]).process(css, {
    from: "input.css", to: "output.css", map: { inline: false }
  });
  const consumer = new SourceMapConsumer(result.map.toJSON());
  const original = consumer.originalPositionFor({ line: 1, column: 0 });
  assert.equal(original.source, "input.css");
  assert.equal(original.line, 1);
  assert.equal(consumer.sourceContentFor("input.css"), css);
});
