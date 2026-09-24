// Единственное изменяемое состояние SPA. Экранные модули получают его через
// контекст загрузчика и не создают собственные копии фильтров или сессии.
(function initializeStore(global) {
  "use strict";

  const state = {
    me: null,
    categories: [],
    partners: [],
    itCompanies: [],
    agreements: [],
    regionalAuthorities: [],
    view: "dashboard",
    period: "plan",
    year: new Date().getFullYear(),
    categoryCode: null,
    entries: [],
    dashboard: null,
    categoriesError: null,
    partnerID: "",
    agreementID: "",
    agreementPartnerID: "",
    partnerKind: "vuz",
    mentors: [],
    dashboardCategory: "",
    dashboardAudience: "",
    dashboardHideZero: false,
    dashboardSortKey: "fact",
    dashboardSortDirection: "desc",
    reportCategory: "",
    reportRiskFilter: "",
    legalEntityGroups: [],
    entryFilters: {},
  };

  global.CyberCalcStore = Object.freeze({ state });
})(globalThis);
