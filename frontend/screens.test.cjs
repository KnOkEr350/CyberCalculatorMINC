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

// Копирует ли Dockerfile файл index.html-ресурса в /dist: либо файл назван
// явно в `cp ... /dist/...`, либо его каталог копируется целиком `cp -R`.
function shippedByDockerfile(dockerfile, rel) {
  const dir = rel.includes("/") ? rel.split("/")[0] : null;
  return dockerfile
    .split("\n")
    .map((line) => line.trim().replace(/\\$/, "").trim().replace(/^&&\s*/, ""))
    .filter((line) => line.startsWith("cp ") && /\/dist\//.test(line))
    .some((line) => {
      const words = line.split(/\s+/);
      const sources = words.slice(1, -1).filter((word) => !word.startsWith("-"));
      return sources.some((src) => src === rel || (dir !== null && src === dir && words.includes("-R")));
    });
}

// Регрессия: /components/ui.js был подключён в index.html, но не попадал в
// образ nginx — в проде скрипт отдавал 404, CyberCalcUI не определялся, и
// дашборд падал на первом fmtMoney().
test("ships every script and stylesheet referenced by index.html into the container", () => {
  const html = fs.readFileSync(require.resolve("./index.html"), "utf8");
  const dockerfile = fs.readFileSync(require.resolve("./Dockerfile"), "utf8");
  const assets = Array.from(html.matchAll(/<(?:script[^>]*\ssrc|link[^>]*\shref)="\/([^"]+)"/g), (match) => match[1]);
  assert.ok(assets.length > 0, "index.html должен подключать локальные ресурсы");
  for (const rel of assets) {
    assert.ok(fs.existsSync(require.resolve(`./${rel}`)), `${rel} подключён в index.html, но отсутствует в репозитории`);
    assert.ok(shippedByDockerfile(dockerfile, rel), `${rel} подключён в index.html, но Dockerfile не копирует его в /dist`);
  }
});

test("every registered screen owns a lazy entrypoint", () => {
  for (const screen of registry().all) {
    const relativePath = `.${screen.module}`;
    const source = fs.readFileSync(require.resolve(relativePath), "utf8");
    assert.match(source, /export (async function render|const render = renderActivity)/);
  }
});
