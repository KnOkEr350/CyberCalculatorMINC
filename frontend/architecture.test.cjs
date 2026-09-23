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
  for (const label of ["Решение и поручение", "Динамический показатель", "План", "Факт", "Основание расчёта", "Документы"]) {
    assert.match(workspace, new RegExp(label));
  }
  assert.match(workspace, /decision_evidence_/);
  assert.match(workspace, /data-entry-filter="decision_number"/);
});

test("dashboard prioritizes operational indicators and moves analytics below", () => {
  const app = source("./app.js");
  for (const label of ["Партнёры", "Действующие соглашения", "Мероприятия по факту", "Требуют внимания"]) {
    assert.match(app, new RegExp(label));
  }
  for (const removed of ["Обязательный минимум ВО", "Реализация плана", "Подтверждённые расходы", "Утверждённый план"]) {
    assert.doesNotMatch(app, new RegExp(removed));
  }
  assert.ok(app.indexOf("dashboard-primary-kpis") < app.indexOf("dashboard-filter-card"));
});

test("partners page shows confirmed partners and lazy-loads details 4 through 6", () => {
  const workspace = source("./workspace.js");
  for (const label of ["Подтверждённые партнёры", "Юридические лица", "Добавить образовательную организацию", "Требуют решения", "4 · Соглашения", "5 · Учебная структура", "6 · Наставники"]) {
    assert.match(workspace, new RegExp(label));
  }
  assert.match(workspace, /data-partner-expand/);
  assert.match(workspace, /async \(\) => \{[\s\S]*Promise\.all\(\[/);
  assert.doesNotMatch(workspace, /<h2>Состояние справочника<\/h2>/);
  assert.doesNotMatch(workspace, /<h2>Поиск в официальном справочнике<\/h2>/);
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

test("UI-07 TOP-05 TOP-08 ADR-14: TOP-IT entry card shows the sub-registers, RID and the co-financing scale", () => {
  const app = source("./app.js");
  assert.match(app, /function renderTopItemsSection/);
  assert.match(app, /\/entries\/\$\{encodeURIComponent\(entryId\)\}\/top-items/);
  for (const label of ["Неденежная поддержка", "Стипендиаты", "Производственные кейсы", "РИД \\(результаты", "Модель ИИ", "university_share_pct"]) assert.match(app, new RegExp(label));
  assert.match(app, /function topScaleMarkup/);
  assert.match(source("./theme.css"), /\.top-scale/);
});

test("UI-11 SEC-10: settings expose the task dispatcher with route, fallback reason and history", () => {
  const app = source("./app.js");
  assert.match(app, /id: "tasks", label: "Задачи и эскалации"/);
  assert.match(app, /function renderSettingsTasks/);
  for (const path of ["/workflow-tasks?status=", "/reassign", "/complete"]) assert.ok(app.includes(path), path);
  assert.match(app, /Причина обхода/);
});

test("UI-05 UI-06 UI-08: practice, internship and school screens use dedicated registries", () => {
  const workspace = source("./workspace.js");
  for (const name of ["practiceTable", "internshipTable", "schoolTable"]) assert.match(workspace, new RegExp(`function ${name}`));
  for (const label of ["Договор о практической подготовке", "Возраст и нагрузка", "Договор о стажировке", "Справки", "Источник средств", "Акт приёмки"]) assert.match(workspace, new RegExp(label));
  assert.match(workspace, /it_clubs: "ИТ-кружки", teacher_training: "Подготовка учителей", edu_content: "Образовательный контент"/);
  assert.match(workspace, /categoryCode === "employment_practice"/);
});

test("DATA-02 DATA-10: agreement history and group limits are visible in the partner dialogs", () => {
  const workspace = source("./workspace.js");
  assert.match(workspace, /function agreementHistoryMarkup/);
  assert.match(workspace, /\/agreements\/\$\{encodeURIComponent\(button\.dataset\.historyAgreement\)\}\/history/);
  assert.match(workspace, /function groupLimitsMarkup/);
  assert.match(workspace, /over_allocated_rub/);
  assert.match(workspace, /\/limits\?report_year=/);
});

test("UI-07 UI-11 UI-12: TOP-IT registry and settings group dialog are wired, registry headers carry scope", () => {
  const workspace = source("./workspace.js");
  const app = source("./app.js");
  assert.match(workspace, /function topItTable/);
  assert.match(workspace, /categoryCode === "top_it"/);
  assert.match(workspace, /<th scope="col">Риск<\/th>/);
  assert.match(source("./theme.css"), /\.sr-only/);
  assert.match(app, /id="settings-groups"/);
  assert.match(app, /openLegalEntityGroups\(\(\) => renderSettingsOverview\(box\)\)/);
});

test("UI-01 dashboard exposes semester and traffic-light slices from the МЦ template", () => {
  const app = source("./app.js");
  assert.match(app, /id="dash-semester"/);
  assert.match(app, /id="dash-light"/);
  assert.match(app, /function dashboardSliceParams/);
  for (const label of ["Можем подтвердить сейчас", "Подтвердим, есть вопросы", "Низкая вероятность", "Осенние (нечётные)", "Весенние (чётные)"]) assert.ok(app.includes(label), label);
  assert.match(app, /\.\.\.dashboardSliceParams\(\)/);
});

test("UI-11 SEC-07: only the system administrator sees the second-factor policy", () => {
  const app = source("./app.js");
  assert.match(app, /state\.me\.role === "super_admin" \? `<div class="card"><h2>Двухфакторная защита/);
  for (const key of ["mfa_required", "mfa_grace_period_hours"]) assert.ok(app.includes(key), key);
});

test("UI-06 PRA-04: practice registry shows labour-law limits from the server verdict", () => {
  const workspace = source("./workspace.js");
  assert.match(workspace, /function laborLawCell/);
  assert.match(workspace, /Ограничения ТК РФ/);
});

test("UI-05 RISK-03: internship and practice cards expose the legal dispute history and actions", () => {
  const app = source("./app.js");
  assert.match(app, /LEGAL_DISPUTE_CATEGORIES = \["internship", "employment_practice"\]/);
  assert.match(app, /\/entries\/\$\{encodeURIComponent\(entryId\)\}\/legal-disputes/);
  assert.match(app, /action: active \? "lift" : "raise"/);
});

test("UI-08: school screen switches kinds 6/7/8 with tabs bound to the category selector", () => {
  const workspace = source("./workspace.js");
  assert.match(workspace, /role="tablist" aria-label="Виды школьного трека"/);
  assert.match(workspace, /data-school-tab/);
  assert.match(workspace, /select\.dispatchEvent\(new Event\("change"\)\)/);
});

test("МЦ template work statuses are summarised in the internship, practice and OOP registries", () => {
  const workspace = source("./workspace.js");
  assert.match(workspace, /function workStatusSummary/);
  assert.equal((workspace.match(/(?<!function )workStatusSummary\(entries\)/g) || []).length, 3);
  const app = source("./app.js");
  for (const label of ["Поиск кандидата", "Кандидат найден", "Запущена", "Ждём от вуза", "Утверждено вузом"]) assert.ok(app.includes(label), label);
});

test("REPORT-09: TOP-IT card links to the six-sheet АНО АЦ report", () => {
  const app = source("./app.js");
  assert.match(app, /report_type=ano_ac&report_year=/);
  assert.match(app, /Отчёт АНО АЦ \(XLSX, 6 листов\)/);
});
