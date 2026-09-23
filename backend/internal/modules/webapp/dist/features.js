// Runtime feature flags. New screens use a descriptor with `featureFlag` and
// remain hidden until the appliance configuration enables that exact flag.
(function initializeFeatureFlags(global) {
  "use strict";

  const known = Object.freeze([
    "dashboard_v44",
    "partners_v44",
    "teachers",
    "oop_rpd",
    "internships",
    "practice",
    "top_it_ai",
    "schools",
    "ministry_decision",
    "reporting_v44",
    "settings_v44",
  ]);
  const flags = Object.fromEntries(known.map((name) => [name, false]));

  function snapshot() {
    return Object.freeze({ ...flags });
  }

  function enabled(name) {
    return known.includes(name) && flags[name] === true;
  }

  function filter(descriptors) {
    return descriptors.filter(
      (descriptor) => !descriptor.featureFlag || enabled(descriptor.featureFlag),
    );
  }

  async function load(fetchImplementation = global.fetch) {
    if (typeof fetchImplementation !== "function") return snapshot();
    try {
      const response = await fetchImplementation("/api/features", {
        credentials: "same-origin",
        headers: { "X-Cybercalc-Request": "1" },
      });
      if (!response.ok) throw new Error(`feature flags: HTTP ${response.status}`);
      const payload = await response.json();
      if (payload?.version !== 1 || !payload.flags || typeof payload.flags !== "object") {
        throw new Error("feature flags: unsupported response");
      }
      for (const name of known) flags[name] = payload.flags[name] === true;
    } catch (error) {
      for (const name of known) flags[name] = false;
      if (global.console?.warn) global.console.warn("Feature flags unavailable; optional screens are disabled", error);
    }
    return snapshot();
  }

  const api = { known, enabled, filter, load, snapshot };
  api.ready = load();
  global.CyberCalcFeatures = Object.freeze(api);
})(globalThis);

