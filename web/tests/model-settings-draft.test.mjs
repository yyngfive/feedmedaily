import assert from "node:assert/strict";
import test from "node:test";
import {createServer} from "vite";

const server = await createServer({server: {middlewareMode: true}, appType: "custom"});
const {createClassifierModelsDraft, classifierModelsUpdateFromDraft, createProfileModelsDraft, enabledClassifierModelIdsAfterKeyChange} = await server.ssrLoadModule("/src/features/admin/ModelSettingsEditor.tsx");
await server.close();

test("a saved provider key does not re-enable a disabled classifier model", () => {
  const models = {
    default_model_id: "deepseek-flash",
    enabled_model_ids: ["deepseek-flash"],
    models: [
      {id: "deepseek-flash", provider: "deepseek", configured: true},
      {id: "deepseek-v4-pro", provider: "deepseek", configured: true},
    ],
  };
  const draft = createClassifierModelsDraft(models);
  assert.deepEqual(draft.enabledModelIds, ["deepseek-flash"]);
  assert.deepEqual(classifierModelsUpdateFromDraft(draft, models).enabled_model_ids, ["deepseek-flash"]);
});

test("stale enabled models without keys do not block saving a newly entered provider key", () => {
  const models = {
    default_model_id: "mimo-v2.6-flash",
    enabled_model_ids: ["deepseek-flash", "deepseek-v4-pro", "mimo-v2.6-flash"],
    models: [
      {id: "deepseek-flash", provider: "deepseek", configured: false},
      {id: "deepseek-v4-pro", provider: "deepseek", configured: false},
      {id: "mimo-v2.6-flash", provider: "mimo", configured: false},
    ],
  };
  const draft = createClassifierModelsDraft(models);
  assert.deepEqual(draft.enabledModelIds, []);
  assert.deepEqual(enabledClassifierModelIdsAfterKeyChange(draft, models, "deepseek", true),
    ["deepseek-flash", "deepseek-v4-pro"]);
});

test("editing another provider key preserves saved classifier selection", () => {
  const models = {
    default_model_id: "deepseek-flash",
    enabled_model_ids: ["deepseek-flash"],
    models: [
      {id: "deepseek-flash", provider: "deepseek", configured: true},
      {id: "deepseek-v4-pro", provider: "deepseek", configured: true},
      {id: "glm-5.3-flash", provider: "zhipu", configured: false},
      {id: "glm-5.3", provider: "zhipu", configured: false},
    ],
  };
  const draft = createClassifierModelsDraft(models);
  assert.deepEqual(enabledClassifierModelIdsAfterKeyChange(draft, models, "zhipu", true),
    ["deepseek-flash", "glm-5.3-flash", "glm-5.3"]);
  assert.deepEqual(enabledClassifierModelIdsAfterKeyChange(draft, models, "zhipu", false),
    ["deepseek-flash"]);
  assert.deepEqual(enabledClassifierModelIdsAfterKeyChange(draft, models, "deepseek", true),
    ["deepseek-flash"]);
});

test("a saved profile default remains selected while its provider key is missing", () => {
  const profileModels = {
    default_model_id: "deepseek-v4-pro",
    models: [
      {id: "deepseek-flash", configured: true},
      {id: "deepseek-v4-pro", configured: false},
    ],
  };
  assert.equal(createProfileModelsDraft(profileModels).defaultModelId, "deepseek-v4-pro");
});
