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
