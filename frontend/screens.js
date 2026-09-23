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
      facts: ["Нагрузка по семестрам", "Проверка стажа и ОКЗ", "Документы преподавателя"],
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
      facts: ["Матрица РПД × ООП", "Разработка · актуализация · экспертиза", "Документы и эксперт"],
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
      facts: ["Часы студента и наставника", "Срочный трудовой договор", "HR · Куратор · Юрист"],
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
      facts: ["Только срочный трудовой договор", "Проверка номера и даты", "Отдельно от стажировок"],
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
      facts: ["Только высшее образование", "Контроль порогов участия", "Фактическое исполнение вузом"],
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
      facts: ["Три направления в одном разделе", "Без бюджетного финансирования", "Связь со школой и РОИВ"],
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
      facts: ["Реквизиты решения обязательны", "Динамическая единица измерения", "Первичные документы"],
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
    facts: Object.freeze([...(screen.facts || [])]),
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
