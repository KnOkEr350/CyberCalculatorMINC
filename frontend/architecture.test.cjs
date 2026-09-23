const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");

function source(path) {
  return fs.readFileSync(require.resolve(path), "utf8");
}

test("store and router preserve activity selection invariants", () => {
  const context = vm.createContext({
    console,
    CyberCalcFeatures: { filter(items) { return items; } },
  });
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

test("router rejects screens hidden by feature flags", () => {
  const context = vm.createContext({
    console,
    CyberCalcFeatures: {
      filter(items) { return items.filter((item) => item.featureFlag === "teachers"); },
    },
  });
  vm.runInContext(source("./core/store.js"), context, { filename: "core/store.js" });
  vm.runInContext(source("./screens.js"), context, { filename: "screens.js" });
  vm.runInContext(source("./core/router.js"), context, { filename: "core/router.js" });
  context.CyberCalcRouter.configure(() => {});

  context.CyberCalcRouter.activate("dashboard");
  assert.equal(context.CyberCalcStore.state.view, "teachers");
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

test("enterprise shell keeps topbar/sidebar proportions and active hierarchy", () => {
  const theme = source("./theme.css");
  const shell = source("./shell/app-shell.js");
  assert.match(theme, /--topbar-height:\s*56px/);
  assert.match(theme, /--sidebar-width:\s*248px/);
  assert.match(theme, /\.topbar\s*\{[^}]*grid-template-columns:\s*var\(--sidebar-width\)\s+52px\s+minmax\(220px,\s*1fr\)\s+auto/s);
  assert.match(theme, /\.sidebar\s*\{[^}]*position:\s*fixed[^}]*top:\s*var\(--topbar-height\)[^}]*overflow:\s*hidden auto/s);
  assert.match(theme, /\.primary-nav button\.active\s*\{[^}]*background:\s*rgba\(77,145,220,\.23\)[^}]*color:\s*#fff/s);
  assert.match(theme, /\.primary-nav button\.active::before\s*\{[^}]*background:\s*#69a9ea/s);
  assert.match(shell, /navigationIcon\(screen\.icon\)/);
  assert.match(shell, /11 экранов системы/);
});

test("Ministry decision screen exposes plan, fact, dynamic metric and evidence", () => {
  const workspace = source("./workspace.js");
  assert.match(workspace, /function ministryDecisionTable/);
  for (const label of ["Решение и поручение", "Динамический показатель", "План", "Факт", "Подтверждённая стоимость", "Документы"]) {
    assert.match(workspace, new RegExp(label));
  }
  assert.match(workspace, /decision_evidence_/);
  assert.match(workspace, /data-entry-filter="decision_number"/);
});

test("teaching screen exposes semester, risk, hours and compensation registry", () => {
  const workspace = source("./workspace.js");
  assert.match(workspace, /function teachingWorkloadTable/);
  for (const label of ["Преподаватель", "Дисциплина", "Программа и семестр", "Ак. часы", "Компенсация"]) {
    assert.match(workspace, new RegExp(label));
  }
  assert.match(workspace, /data-entry-filter="semester"/);
  assert.match(workspace, /teaching-payouts\?year=/);
});

test("OOP and RPD screen exposes permanent 2 by 3 matrix and document registry", () => {
  const workspace = source("./workspace.js");
  assert.match(workspace, /function oopMatrix/);
  assert.match(workspace, /function oopRegistryTable/);
  for (const label of ["Разработка", "Актуализация", "Экспертиза", "Краткая матричная выжимка", "Программа или дисциплина"]) {
    assert.match(workspace, new RegExp(label));
  }
  assert.match(workspace, /\[\["rpd", "РПД"\], \["oop", "ООП"\]\]/);
});
