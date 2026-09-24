// Registry of the eleven product screens from TZ 4.4.
(function initializeScreens(global) {
  "use strict";

  const screens = [
    {
      id: "dashboard",
      number: 1,
      label: "Дашборд",
      title: "Пульс проекта",
      icon: "dashboard",
      module: "/screens/dashboard/index.js",
      featureFlag: "dashboard_v44",
    },
    {
      id: "partners",
      number: 2,
      label: "Партнёры",
      title: "Партнёры и соглашения",
      icon: "partners",
      module: "/screens/partners/index.js",
      featureFlag: "partners_v44",
    },
    {
      id: "teachers",
      number: 3,
      label: "Преподаватели",
      title: "Преподаватели",
      icon: "teachers",
      module: "/screens/teachers/index.js",
      featureFlag: "teachers",
      categoryCodes: ["teachers"],
      audiences: ["vuz", "kolledj"],
      defaultAudience: "vuz",
      kind: "Вид 1",
    },
    {
      id: "ood_rpd",
      number: 4,
      label: "ООП / РПД",
      title: "ООП и РПД",
      icon: "documents",
      module: "/screens/ood-rpd/index.js",
      featureFlag: "oop_rpd",
      categoryCodes: ["ood_rpd"],
      audiences: ["vuz", "kolledj"],
      defaultAudience: "vuz",
      kind: "Вид 3",
    },
    {
      id: "internship",
      number: 5,
      label: "Стажировки",
      title: "Стажировки",
      icon: "internship",
      module: "/screens/internship/index.js",
      featureFlag: "internships",
      categoryCodes: ["internship"],
      audiences: ["vuz", "kolledj"],
      defaultAudience: "vuz",
      kind: "Вид 2",
    },
    {
      id: "employment_practice",
      number: 6,
      label: "Практика",
      title: "Практика с трудоустройством",
      icon: "practice",
      module: "/screens/employment-practice/index.js",
      featureFlag: "practice",
      categoryCodes: ["employment_practice"],
      audiences: ["vuz", "kolledj"],
      defaultAudience: "vuz",
      kind: "Вид 2",
    },
    {
      id: "top_it",
      number: 7,
      label: "ТОП-ИТ / ИИ",
      title: "ТОП-ИТ и ТОП-ИИ",
      icon: "top",
      module: "/screens/top-it/index.js",
      featureFlag: "top_it_ai",
      categoryCodes: ["top_it"],
      audiences: ["vuz"],
      defaultAudience: "vuz",
      kind: "Вид 4",
    },
    {
      id: "schools",
      number: 8,
      label: "Школы",
      title: "Школьный трек",
      icon: "schools",
      module: "/screens/schools/index.js",
      featureFlag: "schools",
      categoryCodes: ["it_clubs", "teacher_training", "edu_content"],
      audiences: ["school"],
      defaultAudience: "school",
      kind: "Виды 6–8",
    },
    {
      id: "minc_decision",
      number: 9,
      label: "Решение МЦ",
      title: "Решение Минцифры",
      icon: "ministry",
      module: "/screens/minc-decision/index.js",
      featureFlag: "ministry_decision",
      categoryCodes: ["minc_decision"],
      audiences: ["vuz", "kolledj"],
      defaultAudience: "vuz",
      kind: "Вид 5",
    },
    {
      id: "reports",
      number: 10,
      label: "Отчётность",
      title: "Центр отчётности",
      icon: "reports",
      module: "/screens/reports/index.js",
      featureFlag: "reporting_v44",
    },
    {
      id: "settings",
      number: 11,
      label: "Настройки",
      title: "Настройки системы",
      icon: "settings",
      module: "/screens/settings/index.js",
      featureFlag: "settings_v44",
    },
  ].map((screen) => Object.freeze({
    ...screen,
    categoryCodes: Object.freeze([...(screen.categoryCodes || [])]),
    audiences: Object.freeze([...(screen.audiences || [])]),
  }));

  const byID = new Map(screens.map((screen) => [screen.id, screen]));
  const available = () => global.CyberCalcFeatures?.filter
    ? global.CyberCalcFeatures.filter(screens)
    : screens;
  const api = {
    all: Object.freeze(screens),
    available,
    get(id) {
      return byID.get(id) || null;
    },
    getAvailable(id) {
      return available().find((screen) => screen.id === id) || null;
    },
    activity(id) {
      const screen = byID.get(id);
      return screen?.categoryCodes.length ? screen : null;
    },
  };

  global.CyberCalcScreens = Object.freeze(api);
})(globalThis);
