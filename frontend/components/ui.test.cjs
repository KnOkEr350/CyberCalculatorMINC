const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const context = { Intl, console, setTimeout: (callback) => { callback(); return 1; } };
context.globalThis = context;
vm.runInNewContext(readFileSync(join(__dirname, "ui.js"), "utf8"), context);
const ui = context.CyberCalcUI;

test("money input uses decimal contract and parses kopecks", () => {
  assert.equal(ui.parseMoney("1 250,40"), 125040);
  assert.equal(ui.parseMoney("12.345"), null);
  assert.match(ui.moneyInput({ id: "amount", label: "Сумма", required: true }), /inputmode="decimal"/);
});

test("date input has an explicit accessible label", () => {
  const html = ui.dateInput({ id: "effective", label: "Действует с", min: "2026-01-01" });
  assert.match(html, /label for="effective"/);
  assert.match(html, /type="date"/);
});

test("risk badge validates state and escapes reasons", () => {
  const html = ui.riskBadge({ state: "green", reasons: ["Документ <проверен>"] });
  assert.match(html, /data-risk="green"/);
  assert.match(html, /Документ &lt;проверен&gt;/);
});

test("icon registry returns own accessible SVG icons with fallback", () => {
  const dashboard = ui.icon("dashboard", { title: "Дашборд" });
  assert.match(dashboard, /aria-label="Дашборд"/);
  assert.match(dashboard, /<title>Дашборд<\/title>/);
  assert.match(ui.icon("missing"), /aria-hidden="true"/);
});

test("filters connect labels and controls", () => {
  const html = ui.filters({ fields: [{ name: "q", label: "Поиск" }, { name: "status", label: "Статус", type: "select", options: ["all", "active"], value: "active" }] });
  assert.match(html, /fieldset/);
  assert.match(html, /label for="q"/);
  assert.match(html, /value="active" selected/);
});

test("toggle, compound fields and inline actions expose accessible controls", () => {
  const toggle = ui.toggle({ id: "hide-zero", label: "Скрыть нули", checked: true, hint: "Не показывать пустые строки" });
  assert.match(toggle, /role="switch"/);
  assert.match(toggle, /checked/);
  assert.match(toggle, /aria-describedby="hide-zero-hint"/);

  const compound = ui.compoundField({ id: "contract", label: "Договор", required: true, parts: [
    { name: "number", label: "Номер", value: "ПР-12" },
    { name: "date", label: "Дата", type: "date" },
  ] });
  assert.match(compound, /<fieldset class="ui-compound-field"/);
  assert.match(compound, /<legend>Договор \*<\/legend>/);
  assert.match(compound, /label for="contract-number"/);

  const actions = ui.inlineActions({ label: "Операции строки", actions: [{ name: "download", label: "Скачать", icon: "download" }] });
  assert.match(actions, /role="group"/);
  assert.match(actions, /aria-label="Операции строки"/);
  assert.match(actions, /data-action="download"/);
});

test("validation summary distinguishes blocking errors and warnings", () => {
  const error = ui.validationSummary({ errors: ["Нет договора"] });
  assert.match(error, /role="alert"/);
  assert.match(error, /Нет договора/);
  const warning = ui.validationSummary({ warnings: ["Нет справки"] });
  assert.match(warning, /role="status"/);
  assert.equal(ui.validationSummary({}), "");
});

test("table renders sort semantics, selected row and empty state", () => {
  const html = ui.table({ caption: "Сотрудники", columns: [{ key: "name", label: "Имя", sortable: true }], rows: [{ id: "1", name: "Иванов" }], selectedKey: "1", sort: { key: "name", direction: "asc" } });
  assert.match(html, /aria-sort="ascending"/);
  assert.match(html, /aria-selected="true"/);
  assert.match(ui.table({ columns: [{ key: "name" }], rows: [] }), /Нет данных/);
});

test("drawer is modal and exposes a labelled close action", () => {
  const html = ui.drawerMarkup({ id: "details", title: "Карточка", content: "<p>Тело</p>", wide: true, mode: "view" });
  assert.match(html, /role="dialog"/);
  assert.match(html, /aria-labelledby="details-title"/);
  assert.match(html, /ui-drawer-wide/);
  assert.match(html, /data-drawer-mode="view"/);
  assert.match(html, /data-drawer-close/);
});

test("upload is keyboard reachable and restricts accepted files", () => {
  const html = ui.uploadField({ id: "source", accept: ".pdf", required: true });
  assert.match(html, /role="button"/);
  assert.match(html, /tabindex="0"/);
  assert.match(html, /accept=".pdf"/);
});

test("upload supports keyboard selection and file drop", () => {
  const listeners = {};
  const inputListeners = {};
  const input = {
    files: [], value: "", clicks: 0,
    click() { this.clicks += 1; },
    addEventListener(type, listener) { inputListeners[type] = listener; },
  };
  const zone = {
    classList: { add() {}, remove() {} },
    addEventListener(type, listener) { listeners[type] = listener; },
  };
  const root = { querySelector(selector) { return selector.includes("input") ? input : zone; } };
  let received = [];
  const control = ui.bindUpload(root, (files) => { received = files; });
  listeners.keydown({ key: "Enter", preventDefault() {} });
  assert.equal(input.clicks, 1);
  listeners.drop({ preventDefault() {}, dataTransfer: { files: [{ name: "official.pdf" }] } });
  assert.equal(received[0].name, "official.pdf");
  assert.equal(control.files[0].name, "official.pdf");
});

test("reorder helper is immutable and supports boundary no-op", () => {
  const original = ["a", "b", "c"];
  assert.deepEqual(Array.from(ui.moveItem(original, 2, 0)), ["c", "a", "b"]);
  assert.deepEqual(Array.from(ui.moveItem(original, 0, -1)), original);
  assert.deepEqual(original, ["a", "b", "c"]);
});

test("reorder exposes keyboard and drag-and-drop alternatives", () => {
  const listeners = {};
  const row = (key) => ({
    dataset: { reorderKey: key },
    closest(selector) { return selector === "[data-reorder-key]" ? this : null; },
    setAttribute() {},
  });
  const first = row("first");
  const second = row("second");
  const root = {
    addEventListener(type, listener) { listeners[type] = listener; },
    querySelectorAll() { return [first, second]; },
  };
  const moves = [];
  ui.bindReorder(root, (from, to, key) => moves.push([from, to, key]));
  listeners.keydown({ altKey: true, key: "ArrowDown", target: first, preventDefault() {} });
  listeners.dragstart({ target: first });
  listeners.drop({ target: second, preventDefault() {} });
  assert.deepEqual(moves, [[0, 1, "first"], [0, 1, "first"]]);
});

test("toast uses status for success and alert for errors", () => {
  const created = [];
  const document = {
    body: { appendChild: (item) => created.push(item) },
    createElement: () => ({ classList: { add() {}, remove() {} }, setAttribute(name, value) { this[name] = value; }, remove() {} }),
  };
  assert.equal(ui.toast("Готово", "success", { document, duration: 0 }).role, "status");
  assert.equal(ui.toast("Ошибка", "error", { document, duration: 0 }).role, "alert");
  assert.equal(created.length, 2);
});
