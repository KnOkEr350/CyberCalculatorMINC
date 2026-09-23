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

test("shell navigation is permission-aware without hiding read-only screens", () => {
  const context = vm.createContext({});
  vm.runInContext(source("./screens.js"), context, { filename: "screens.js" });
  vm.runInContext(source("./shell/app-shell.js"), context, { filename: "shell/app-shell.js" });

  const screens = context.CyberCalcScreens;
  const hr = { role: "hr_specialist", entity_type: "organization" };
  assert.equal(context.screenAccess(screens.get("internship"), hr).mode, "write");
  assert.equal(context.screenAccess(screens.get("employment_practice"), hr).mode, "write");
  assert.equal(context.screenAccess(screens.get("teachers"), hr).mode, "read");
  assert.equal(context.screenAccess(screens.get("reports"), { role: "auditor_viewer", entity_type: "organization" }).mode, "read");
  assert.equal(context.screenAccess(screens.get("settings"), { role: "org_admin", entity_type: "organization" }).mode, "admin");
});

test("shell sidebar supports keyboard roving focus commands", () => {
  const shell = source("./shell/app-shell.js");
  assert.match(shell, /bindPrimaryNavKeyboard\(wrap\.querySelector\("\.primary-nav"\)\)/);
  for (const key of ["ArrowDown", "ArrowUp", "Home", "End"]) {
    assert.match(shell, new RegExp(`event\\.key === "${key}"`));
  }
});

test("product shell locks page scroll and delegates overflow to workspace", () => {
  const theme = source("./theme.css");
  const legacy = source("./style.css");
  assert.match(legacy, /body\s*\{[^}]*margin:\s*0/s, "browser body margin must not create page scroll");
  assert.match(theme, /\.app-shell\s*\{[^}]*position:\s*fixed[^}]*inset:\s*0[^}]*height:\s*100vh[^}]*overflow:\s*hidden/s);
  assert.match(theme, /\.app-body\s*\{[^}]*height:\s*100vh[^}]*min-height:\s*0/s);
  assert.match(theme, /\.container\s*\{[^}]*height:\s*calc\(100vh - var\(--topbar-height\)\)[^}]*overflow:\s*auto/s);
});
