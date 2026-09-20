import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/features/review/journalAliases.ts", import.meta.url), "utf8");
const {outputText} = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.ESNext}});
const {journalDisplay, journalKey, toggleJournalSelection} = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

test("paper surfaces prefer the backend identity while retaining legacy fallback", () => {
  const paper = {journal: "ACS Nano advanceAccess", journal_display: "ACS Nano", journal_key: "journal:abc", feed_title: "ACS Nano"};
  assert.equal(journalDisplay(paper), "ACS Nano");
  assert.equal(journalKey(paper), "journal:abc");
  assert.equal(journalDisplay({journal: "Custom Journal"}), "Custom Journal");
  assert.equal(journalDisplay({feed_title: "Custom Feed"}), "Custom Feed");
  assert.equal(journalDisplay({}), "Unknown journal");
});

test("journal selection toggles stable backend keys", () => {
  const old = ["journal:cell", "journal:nature"];
  assert.deepEqual(toggleJournalSelection(old, "journal:cell"), ["journal:nature"]);
  assert.deepEqual(toggleJournalSelection(old, "journal:acs"), [...old, "journal:acs"]);
  assert.equal(old.length, 2);
});
