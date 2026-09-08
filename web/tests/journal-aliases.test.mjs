import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/features/review/journalAliases.ts", import.meta.url), "utf8");
const {outputText} = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.ESNext}});
const {journalAlias, journalSelection, toggleJournalSelection} = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

test("ACS advance access and Cell issues share their base journal", () => {
  for (const name of ["ACS Central Science", "ACS Nano", "ACS Applied Materials & Interfaces", "Analytical Chemistry", "Journal of the American Chemical Society"]) {
    assert.equal(journalAlias(`${name} advanceAccess`), name);
    assert.equal(journalAlias(name), name);
  }
  for (const issue of ["10", "11", "12", "13", "10–11"]) {
    assert.equal(journalAlias(`Cell, Volume 189, Issue ${issue}`), "Cell");
  }
});

test("unrecognized journals and suffixes are preserved", () => {
  for (const name of ["Cell Reports", "Cell Research", "Cell Reports, Volume 1, Issue 2", "Nature advanceAccess", "ACS Nano advanceAccess extra", "Cell, Volume biology", "Cellular advanceAccess"]) {
    assert.equal(journalAlias(name), name);
  }
  assert.equal(journalAlias(null), "");
  assert.equal(journalAlias(undefined), "");
});

test("legacy selections merge and toggle off the whole journal group", () => {
  const old = ["Cell", "Cell, Volume 189, Issue 10", "ACS Nano advanceAccess"];
  assert.deepEqual(journalSelection(old), ["ACS Nano", "Cell"]);
  assert.deepEqual(toggleJournalSelection(old, "Cell"), ["ACS Nano"]);
  assert.deepEqual(toggleJournalSelection([], "ACS Nano advanceAccess"), ["ACS Nano"]);
  assert.equal(old.length, 3);
});

test("merged selection returns the union without mutating original metadata", () => {
  const papers = ["Cell", "Cell, Volume 189, Issue 10", "Cell, Volume 189, Issue 11", "Cell Reports"].map((journal, id) => ({id, journal}));
  const before = JSON.stringify(papers);
  const selected = new Set(journalSelection(["Cell, Volume 189, Issue 10"]));
  assert.deepEqual(papers.filter((paper) => selected.has(journalAlias(paper.journal))).map((paper) => paper.id), [0, 1, 2]);
  assert.equal(JSON.stringify(papers), before);
});
