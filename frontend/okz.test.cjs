const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const test = require("node:test");
const assert = require("node:assert/strict");

function loadModule() {
  const source = fs.readFileSync(path.join(__dirname, "okz.js"), "utf8");
  const context = { URLSearchParams };
  context.globalThis = context;
  vm.runInNewContext(source, context);
  return context.CyberCalcOKZ;
}

test("validates OKZ codes from one to four digits", () => {
  const module = loadModule();
  for (const value of ["2", "25", "251", "2512", "0110"]) assert.equal(module.validCode(value), true);
  for (const value of ["", "25123", "25.1", "A251", " 25"]) assert.equal(module.validCode(value), false);
});

test("builds encoded paginated search path", () => {
  const module = loadModule();
  assert.equal(
    module.buildSearchPath({ query: " разработчик ПО ", level: 4, limit: 50, offset: 100 }),
    "/okz?q=%D1%80%D0%B0%D0%B7%D1%80%D0%B0%D0%B1%D0%BE%D1%82%D1%87%D0%B8%D0%BA+%D0%9F%D0%9E&level=4&limit=50&offset=100",
  );
});
