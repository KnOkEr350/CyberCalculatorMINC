const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");

const source = fs.readFileSync(require.resolve("./features.js"), "utf8");

function execute(fetchImplementation) {
  const context = vm.createContext({
    console: { warn() {} },
    fetch: fetchImplementation,
  });
  vm.runInContext(source, context, { filename: "features.js" });
  return context.CyberCalcFeatures;
}

test("loads known frontend flags and filters screen descriptors", async () => {
  const features = execute(async () => ({
    ok: true,
    json: async () => ({ version: 1, flags: { teachers: true, unknown: true } }),
  }));
  await features.ready;

  assert.equal(features.enabled("teachers"), true);
  assert.equal(features.enabled("schools"), false);
  assert.equal(features.enabled("unknown"), false);
  assert.deepEqual(
    Array.from(features.filter([
      { id: "legacy" },
      { id: "teachers", featureFlag: "teachers" },
      { id: "schools", featureFlag: "schools" },
    ]), (item) => item.id),
    ["legacy", "teachers"],
  );
});

test("fails closed when configuration cannot be loaded", async () => {
  const features = execute(async () => {
    throw new Error("offline");
  });
  const snapshot = await features.ready;
  assert.equal(features.enabled("teachers"), false);
  assert.equal(Object.values(snapshot).some(Boolean), false);
});

