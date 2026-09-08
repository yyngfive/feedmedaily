import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

// Run the production helper with the installed TypeScript compiler; no extra test dependencies.
const source = await readFile(new URL("../src/features/profile/topicDraft.ts", import.meta.url), "utf8");
const {outputText} = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.ESNext}});
const {relabelRuleTopics} = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

for (const persisted of [true, false]) {
  const topics = ["A", "B", "C"].map((label) => ({id: persisted ? label : "", label, draftKey: label}));
  const rules = topics.map(({label}) => ({text: label, topics: [label]}));
  for (let index = 0; index < topics.length; index++) {
    test(`delete topic ${index}, persisted=${persisted}`, () => {
      const result = relabelRuleTopics(rules, topics, topics.filter((_, i) => i !== index));
      assert.deepEqual(result.map((rule) => rule.topics), topics.map(({label}, i) => i === index ? [] : [label]));
      assert.deepEqual(rules.map((rule) => rule.topics), [["A"], ["B"], ["C"]]);
    });
  }
  test(`rename and reorder retain rule identity, persisted=${persisted}`, () => {
    const next = [{...topics[2], label: "Renamed"}, topics[0], topics[1]];
    assert.deepEqual(relabelRuleTopics(rules, topics, next).map((rule) => rule.topics), [["A"], ["B"], ["Renamed"]]);
  });
}
