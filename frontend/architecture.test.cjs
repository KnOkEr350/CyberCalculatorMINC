const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");

function source(path) {
  return fs.readFileSync(require.resolve(path), "utf8");
}

test("store and router preserve activity selection invariants", () => {
  const context = vm.createContext({ console });
  vm.runInContext(source("./core/store.js"), context, { filename: "core/store.js" });
  vm.runInContext(source("./screens.js"), context, { filename: "screens.js" });
  vm.runInContext(source("./core/router.js"), context, { filename: "core/router.js" });

  let renders = 0;
  context.CyberCalcRouter.configure(() => { renders += 1; });
  const state = context.CyberCalcStore.state;
  state.categoryCode = "teachers";
  state.partnerKind = "school";
  state.partnerID = "school-id";
  state.agreementID = "agreement-id";

  context.CyberCalcRouter.activate("top_it");
  assert.equal(state.view, "top_it");
  assert.equal(state.categoryCode, "top_it");
  assert.equal(state.partnerKind, "vuz");
  assert.equal(state.partnerID, "");
  assert.equal(state.agreementID, "");
  assert.equal(renders, 1);
});

test("html loads store, router, loader and shell before bootstrap", () => {
  const html = source("./index.html");
  const expectedOrder = [
    "/core/store.js",
    "/screens.js",
    "/core/router.js",
    "/core/screen-loader.js",
    "/shell/app-shell.js",
    "/app.js",
  ];
  let previous = -1;
  for (const path of expectedOrder) {
    const offset = html.indexOf(`src="${path}"`);
    assert.ok(offset > previous, `${path} must be loaded in dependency order`);
    previous = offset;
  }
});

test("shell delegates content rendering to the lazy screen loader", () => {
  const shell = source("./shell/app-shell.js");
  assert.match(shell, /CyberCalcRouter\.activate\(button\.dataset\.view\)/);
  assert.match(shell, /CyberCalcScreenLoader\.render\(state\.view, content\)/);
  assert.doesNotMatch(shell, /state\.view === "dashboard"/);
});
