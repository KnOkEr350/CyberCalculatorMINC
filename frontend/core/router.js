// Маршрутизатор изменяет только store. Повторный render передаётся bootstrap-
// модулем, поэтому router не зависит от реализации shell или экранов.
(function initializeRouter(global) {
  "use strict";

  let renderApplication = null;

  function configure(render) {
    if (typeof render !== "function") throw new TypeError("router render callback is required");
    renderApplication = render;
  }

  function activate(view) {
    const state = global.CyberCalcStore.state;
    const registry = global.CyberCalcScreens;
    const screen = registry.getAvailable(view) || registry.available()[0];
    if (!screen) throw new Error("Нет включённых экранов");
    const activity = registry.activity(screen.id);
    state.view = screen.id;

    if (activity) {
      if (!activity.categoryCodes.includes(state.categoryCode)) {
        state.categoryCode = activity.categoryCodes[0];
        state.entryFilters = {};
        state.entryPageOffset = 0;
      }
      if (activity.audiences.length && !activity.audiences.includes(state.partnerKind)) {
        state.partnerKind = activity.defaultAudience || activity.audiences[0];
        const selectedPartner = state.partners.find((partner) => partner.id === state.partnerID);
        if (!selectedPartner || !activity.audiences.includes(selectedPartner.partner_kind)) {
          state.partnerID = "";
          state.agreementID = "";
          state.agreementPartnerID = "";
        }
      }
    }

    if (!renderApplication) throw new Error("router is not configured");
    renderApplication();
  }

  global.CyberCalcRouter = Object.freeze({ activate, configure });
})(globalThis);
