import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/app/messages.ts", import.meta.url), "utf8");
const {outputText} = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.ESNext}});
const {createAppUpdateMessage} = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

test("an available update creates a persistent warning for the shared message bar", () => {
  const message = createAppUpdateMessage({has_update: true, latest_version: "0.7.0"});
  assert.equal(message?.name, "app.update.available");
  assert.equal(message?.text, "Version 0.7.0 is available. Open Settings → App to download it.");
  assert.equal(message?.tone, "warning");
  assert.equal(message?.ttlMs, 0);
});

test("missing or unavailable update versions do not create a message", () => {
  assert.equal(createAppUpdateMessage({has_update: false, latest_version: "0.7.0"}), null);
  assert.equal(createAppUpdateMessage({has_update: true, latest_version: null}), null);
  assert.equal(createAppUpdateMessage({has_update: true, latest_version: "  "}), null);
});
