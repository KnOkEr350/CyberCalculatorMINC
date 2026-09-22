const assert = require("node:assert/strict");
const fs = require("node:fs");
const test = require("node:test");
const vm = require("node:vm");

const source = fs.readFileSync(require.resolve("./screens.js"), "utf8");

function registry() {
  const context = vm.createContext({});
  vm.runInContext(source, context, { filename: "screens.js" });
  return context.CyberCalcScreens;
}

test("registers the eleven TZ screens in menu order", () => {
  const screens = registry().all;
  assert.equal(screens.length, 11);
  assert.deepEqual(Array.from(screens, (screen) => screen.number), [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]);
  assert.equal(new Set(Array.from(screens, (screen) => screen.id)).size, 11);
  assert.equal(new Set(Array.from(screens, (screen) => screen.module)).size, 11);
  for (const screen of screens) {
    assert.match(screen.module, /^\/screens\/[a-z0-9-]+\/index\.js$/);
  }
});

test("routes every activity category to its own product screen", () => {
  const screens = registry();
  const categories = Array.from(screens.all, (screen) => Array.from(screen.categoryCodes)).flat();
  assert.deepEqual(categories.sort(), [
    "edu_content",
    "employment_practice",
    "internship",
    "it_clubs",
    "minc_decision",
    "ood_rpd",
    "teacher_training",
    "teachers",
    "top_it",
  ]);
  assert.deepEqual(Array.from(screens.get("schools").categoryCodes), ["it_clubs", "teacher_training", "edu_content"]);
});

test("ships the screen registry in the browser and container", () => {
  const html = fs.readFileSync(require.resolve("./index.html"), "utf8");
  const dockerfile = fs.readFileSync(require.resolve("./Dockerfile"), "utf8");
  assert.match(html, /<script src="\/screens\.js"><\/script>/);
  assert.match(dockerfile, /screens\.js/);
  assert.match(dockerfile, /COPY screens \.\/screens/);
  assert.match(dockerfile, /cp -R core shell screens \/dist\//);
});

test("every registered screen owns a lazy entrypoint", () => {
  for (const screen of registry().all) {
    const relativePath = `.${screen.module}`;
    const source = fs.readFileSync(require.resolve(relativePath), "utf8");
    assert.match(source, /export (async function render|const render = renderActivity)/);
  }
});
