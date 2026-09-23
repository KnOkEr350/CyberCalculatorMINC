// Экран загружается только при первом открытии. Import promise кэшируется,
// поэтому повторная навигация не выполняет модуль заново.
(function initializeScreenLoader(global) {
  "use strict";

  const modules = new Map();
  let contextFactory = null;

  function configure(factory) {
    if (typeof factory !== "function") throw new TypeError("screen context factory is required");
    contextFactory = factory;
  }

  function load(screen) {
    if (!screen?.module || !screen.module.startsWith("/screens/")) {
      return Promise.reject(new Error(`Для экрана ${screen?.id || "unknown"} не задан модуль`));
    }
    if (!modules.has(screen.id)) modules.set(screen.id, import(screen.module));
    return modules.get(screen.id);
  }

  async function render(screenID, root) {
    if (!contextFactory) throw new Error("screen loader is not configured");
    const screen = global.CyberCalcScreens.getAvailable(screenID);
    if (!screen) throw new Error(`Неизвестный экран: ${screenID}`);
    root.setAttribute("aria-busy", "true");
    try {
      const module = await load(screen);
      if (typeof module.render !== "function") throw new Error(`Модуль ${screen.id} не экспортирует render()`);
      await module.render(root, contextFactory(), screen);
    } finally {
      root.removeAttribute("aria-busy");
    }
  }

  global.CyberCalcScreenLoader = Object.freeze({ configure, load, render });
})(globalThis);
