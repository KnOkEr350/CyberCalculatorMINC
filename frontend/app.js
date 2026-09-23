// Калькулятор затрат по Приказу Минцифры — bootstrap и общие экранные
// операции. Store, router, shell и lazy entrypoints экранов вынесены отдельно.
const state = CyberCalcStore.state;

const CATEGORY_LABELS = {}; // заполняется из /api/categories
const AUDIENCE_LABELS = { vuz: "Вуз", kolledj: "СПО", school: "Школьный трек" };
const AGREEMENT_STATUS_LABELS = {
  needs_review: "Требует проверки",
  draft: "Проект",
  active: "Действует",
  suspended: "Приостановлено",
  expired: "Истекло",
  terminated: "Расторгнуто",
};
const AGREEMENT_KIND_LABELS = {
  education_organization: "С образовательной организацией",
  roiv: "С РОИВ",
};
const CHART_COLORS = [
  "#397ec4",
  "#43a98b",
  "#6c75c9",
  "#dda33f",
  "#c86686",
  "#71a15d",
  "#5c9fd6",
  "#8a69b4",
  "#c97c4f",
];

const VALUE_LABELS = {
  vuz: "Вуз",
  kolledj: "Колледж",
  school: "Школа",
  rpd: "РПД",
  oop: "ООП",
  vo: "Высшее образование (ВО)",
  spo: "Среднее профессиональное образование (СПО)",
  bachelor: "Бакалавриат",
  master: "Магистратура",
  specialist: "Специалитет",
  absent: "Отсутствует",
  full_or_partial: "Есть полностью или частично",
  fixed_term: "Срочный трудовой договор",
  other: "Другой тип договора",
  president_instruction: "Поручение Президента РФ",
  government_instruction: "Поручение Правительства РФ",
  curator_instruction: "Поручение куратора Министерства",
  security_council_decision: "Решение Совета Безопасности РФ",
  president: "Президент РФ",
  prime_minister: "Председатель Правительства РФ",
  deputy_prime_minister: "Куратор Министерства",
  security_council: "Совет Безопасности РФ",
  development: "Разработка",
  update: "Актуализация",
  expertise: "Экспертиза",
  assistance: "Содействие",
  cofinancing: "Софинансирование",
  user: "Пользователь",
  moderator: "Модератор",
  admin: "Администратор",
  super_admin: "Главный администратор",
  holding_admin: "Администратор холдинга",
  org_admin: "Администратор организации",
  curator: "Куратор",
  hr_specialist: "Кадровая служба (HR)",
  financial_specialist: "Финансовая служба",
  legal_specialist: "Юридическое управление",
  auditor_viewer: "Аудитор (только чтение)",
  organization: "ИТ-организация",
  edu_institution: "Образовательная организация",
  entry: "Запись",
  attachment: "Документ",
  partner: "Партнёр",
  settings: "Настройки",
  education_directory: "Учебное заведение",
  create: "Создание",
  upload: "Загрузка файла",
  login: "Вход",
  delete: "Удаление",
  settings_change: "Изменение настроек",
  directory_enrich: "Автоматический подбор реквизитов",
  directory_programs: "Обновление направлений подготовки",
  directory_update: "Изменение реквизитов",
  directory_confirm: "Подтверждение учебного заведения",
  directory_create: "Добавление учебного заведения",
  directory_propose: "Предложение учебного заведения",
  directory_proposal_approve: "Принятие предложения",
  directory_proposal_reject: "Отклонение предложения",
  okz_catalog_version: "Версия ОКЗ",
  okz_import: "Импорт ОКЗ",
  academic_group: "Академическая группа",
  agreement: "Соглашение",
  curator_assignment: "Закрепление куратора",
  directory: "Справочник",
  it_company: "ИТ-компания",
  legal_dispute: "Юридическое сомнение",
  legal_entity_group: "Группа юридических лиц",
  mentor: "Наставник",
  normative_source: "Нормативный источник",
  org_unit: "Структурное подразделение",
  regional_authority: "РОИВ",
  regulatory_process: "Регламентный процесс",
  report: "Отчёт",
  report_snapshot: "Снимок отчёта",
  specialty_catalog: "Справочник специальностей",
  staff_member: "Сотрудник",
  tariff: "Тариф",
  teaching_payout: "Выплата преподавателю",
  workflow_task: "Задача",
};

function valueLabel(value) {
  return VALUE_LABELS[value] || value;
}

const AUDIT_ACTION_LABELS = {
  update: "Изменение",
  metadata: "Изменение реквизитов документа",
  review: "Проверка",
  assign: "Закрепление",
  end: "Завершение периода",
  revoke: "Отзыв",
  reassign: "Переназначение",
  complete: "Выполнение",
  import: "Импорт",
  export: "Выгрузка",
  status: "Смена статуса",
  seal: "Формирование снимка",
  tariff_publish: "Публикация тарифа",
  mfa_enabled: "Включение двухфакторной защиты",
  mfa_reset: "Сброс двухфакторной защиты",
  password_change: "Смена пароля",
  legal_dispute_raise: "Постановка под сомнение",
  legal_dispute_lift: "Снятие сомнения",
  normative_source_import: "Импорт нормативного источника",
  process_send: "Отправка",
  process_start_review: "Принятие к рассмотрению",
  process_approve: "Согласование",
  process_request_rework: "Возврат на доработку",
  process_resubmit: "Повторная отправка",
  process_dispute: "Оспаривание",
  process_default_approve: "Согласование по молчанию",
  process_rework_lapsed: "Пропуск срока доработки",
};

function actionLabel(value) {
  return AUDIT_ACTION_LABELS[value] || valueLabel(value);
}

async function api(path, opts = {}, pageCount = 0) {
  const res = await fetch("/api" + path, {
    credentials: "same-origin",
    ...opts,
    headers: {
      "X-Cybercalc-Request": "1",
      ...(opts.body && !(opts.body instanceof FormData) ? { "Content-Type": "application/json" } : {}),
      ...(opts.headers || {}),
    },
  });
  if (res.status === 401 && path !== "/auth/login" && path !== "/auth/me") {
    state.me = null;
    render();
    throw new Error("требуется авторизация");
  }
  const isJSON = (res.headers.get("content-type") || "").includes(
    "application/json",
  );
  const data = isJSON ? await res.json().catch(() => null) : null;
  if (!res.ok) {
    throw new Error((data && data.error) || `Ошибка ${res.status}`);
  }
  if (Array.isArray(data) && res.headers.has("X-Next-Offset")) data.nextOffset = Number(res.headers.get("X-Next-Offset"));
  if (Array.isArray(data) && data.nextOffset != null && !["/entries", "/it-companies"].includes(path.split("?")[0])) {
    if (pageCount >= 19) throw new Error("Список превышает 10000 элементов. Сузьте выборку.");
    const next = new URL(path, "http://local"); next.searchParams.set("offset", String(data.nextOffset));
    return data.concat(await api(next.pathname + next.search, opts, pageCount + 1));
  }
  return data;
}

const app = document.getElementById("app");

function el(html) {
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  return t.content.firstElementChild;
}

function fmtMoney(v) {
  return CyberCalcUI.formatMoney(v);
}

function fmtReportDate(value) {
  const date = value ? new Date(value) : new Date();
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "2-digit",
  });
}

function dashboardKPIIcon(kind) {
  return CyberCalcUI.icon(`dashboard-${kind}`);
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function clampPercent(value) {
  const number = Number(value);
  return Number.isFinite(number) ? Math.max(0, Math.min(100, number)) : 0;
}

function validYear(value) {
  return Number.isInteger(value) && value >= 2000 && value <= 2100;
}

function brandMarkup(inverse = false) {
  return `<div class="brand-lockup${inverse ? " inverse" : ""}" aria-label="Киберпротект">
    <span class="brand-emblem" aria-hidden="true"><svg viewBox="0 0 40 40" focusable="false"><path class="brand-shield" d="M20 2 35 8v10.4c0 9.1-6.1 16.5-15 19.6C11.1 34.9 5 27.5 5 18.4V8L20 2Z"/><path class="brand-shine" d="M20 2 5 8v10.4c0 9.1 6.1 16.5 15 19.6V2Z"/><path class="brand-rim" d="M20 4.2 33.2 9.3v9.1c0 8-5.3 14.6-13.2 17.5C12.1 33 6.8 26.4 6.8 18.4V9.3L20 4.2Z"/><path class="brand-cut" d="M27.8 13.2a10 10 0 1 0 0 13.6l-4-4a4.4 4.4 0 1 1 0-5.6l4-4Z"/><path class="brand-core" d="M21.5 16.6a4.5 4.5 0 0 0 0 6.8l-3.7 3.7a9.7 9.7 0 0 1 0-14.2l3.7 3.7Z"/></svg></span>
    <span class="brand-copy"><strong>КИБЕРПРОТЕКТ</strong></span>
  </div>`;
}

function navigationIcon(kind) {
  return CyberCalcUI.icon(kind);
}

function initials(name) {
  return String(name || "П")
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0] || "")
    .join("")
    .toUpperCase();
}

function showToast(message, kind = "error") {
  return CyberCalcUI.toast(message, kind);
}

// ---------------------------------------------------------------- ROUTER --

async function boot() {
  await CyberCalcFeatures.ready;
  try {
    state.me = await api("/auth/me");
  } catch (e) {
    state.me = null;
  }
  if (state.me) {
    try {
      state.categories = (await api("/categories")) || [];
      state.categories.forEach((c) => (CATEGORY_LABELS[c.code] = c.name));
      state.categoriesError = null;
    } catch (e) {
      state.categories = [];
      state.categoriesError = e.message;
    }
    try {
      state.partners = (await api("/partners")) || [];
    } catch (e) {
      state.partners = [];
    }
    if (state.me.entity_type === "organization" && state.me.it_company_id) {
      try { state.legalEntityGroups = ((await api("/legal-entity-groups")) || []).filter((group) => group.status !== "terminated"); }
      catch (_) { state.legalEntityGroups = []; }
    }
  }
  render();
}

function render() {
  app.innerHTML = "";
  if (!state.me) {
    app.appendChild(renderLogin());
    return;
  }
  if (state.me.mfa_required) { app.appendChild(renderMFASetup()); return; }
  if (
    !state.me.entity_type ||
    (state.me.entity_type === "edu_institution" && !state.me.partner_id) ||
    (state.me.role !== "super_admin" &&
      state.me.entity_type === "organization" &&
      !state.me.it_company_id)
  ) {
    app.appendChild(
      el(
        `<main class="system-state"><div class="card"><span class="state-mark" aria-hidden="true">!</span><h1>Профиль не настроен</h1><p>Администратор должен назначить вам роль и организацию.</p><div class="flex"><button class="btn" id="reload-profile">Проверить снова</button><button class="btn secondary" id="unassigned-logout">Выйти</button></div></div></main>`,
      ),
    );
    app.querySelector("#unassigned-logout").onclick = async () => {
      await api("/auth/logout", { method: "POST" });
      state.me = null;
      render();
    };
    app.querySelector("#reload-profile").onclick = () => location.reload();
    return;
  }
  app.appendChild(renderLayout());
}

// ----------------------------------------------------------------- LOGIN --

function renderLogin() {
  const wrap = el(`<div class="auth-shell">
    <aside class="auth-brand-panel">
      ${brandMarkup(true)}
      <div class="auth-message"><h2>Калькулятор затрат</h2></div>
      <div class="auth-orbit" aria-hidden="true"><i></i><i></i><i></i></div>
    </aside>
    <main class="auth-form-panel">
      <form class="login-box" id="login-form">
        <span class="eyebrow">Личный кабинет</span>
        <h1>Вход</h1>
        <div class="field"><label for="login-email">Электронная почта</label><input type="email" id="login-email" autocomplete="username" required placeholder="name@company.ru"></div>
        <div class="field"><label for="login-password">Пароль</label><input type="password" id="login-password" autocomplete="current-password" required placeholder="Пароль"></div>
        <div class="field"><label for="login-code">Код подтверждения <span class="label-optional">необязательно</span></label><input id="login-code" autocomplete="one-time-code" maxlength="20" placeholder="6 цифр или резервный код"></div>
        <div class="error form-message" id="login-error" style="display:none" role="alert"></div>
        <button type="submit" class="btn wide" id="login-submit">Войти</button>
        <p class="auth-help">Нет доступа? Обратитесь к администратору.</p>
      </form>
    </main>
  </div>`);
  wrap.querySelector("#login-form").onsubmit = async (event) => {
    event.preventDefault();
    const email = wrap.querySelector("#login-email").value.trim();
    const password = wrap.querySelector("#login-password").value;
    const errBox = wrap.querySelector("#login-error");
    const submit = wrap.querySelector("#login-submit");
    errBox.style.display = "none";
    submit.disabled = true;
    submit.textContent = "Входим…";
    try {
      await api("/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password, code: wrap.querySelector("#login-code").value.trim() }),
      });
      await boot();
    } catch (e) {
      errBox.textContent = e.message;
      errBox.style.display = "block";
    } finally {
      submit.disabled = false;
      submit.textContent = "Войти";
    }
  };
  return wrap;
}

function openPasswordDialog() {
  const drawer = CyberCalcUI.openDrawer({
    id: "password-drawer",
    title: "Смена пароля",
    mode: "edit",
    content: `
    <p>После смены пароля потребуется войти заново на всех устройствах.</p>
    <form><label>Текущий пароль<input name="current" type="password" autocomplete="current-password" required maxlength="128"></label>
    <label>Новый пароль<input name="next" type="password" autocomplete="new-password" required minlength="10" maxlength="128"></label>
    <label>Повторите новый пароль<input name="repeat" type="password" autocomplete="new-password" required maxlength="128"></label>
    <p>10–128 символов: заглавная и строчная буквы, цифра и специальный символ.</p>
    <p role="alert" class="error"></p><button class="btn" type="submit">Сменить пароль</button><button class="btn secondary" type="button">Отмена</button></form>`,
  });
  const modal = drawer.element;
  const form = modal.querySelector("form");
  form.querySelector('[type="button"]').onclick = drawer.close;
  form.onsubmit = async (event) => {
    event.preventDefault();
    const error = form.querySelector('[role="alert"]');
    if (form.elements.next.value !== form.elements.repeat.value) { error.textContent = "Пароли не совпадают"; return; }
    const button = form.querySelector('[type="submit"]'); button.disabled = true;
    try {
      await api("/auth/password", { method: "POST", body: JSON.stringify({current_password: form.elements.current.value, new_password: form.elements.next.value}) });
      form.reset(); drawer.close(); state.me = null; render(); showToast("Пароль изменён. Войдите с новым паролем.");
    } catch (e) { error.textContent = e.message; } finally { button.disabled = false; }
  };
}

// --------------------------------------------------------- PRODUCT SCREENS --

async function renderPartnersScreen(root) {
  const screen = CyberCalcScreens.get("partners");
  root.innerHTML = `<section class="page-heading screen-heading"><div><span class="eyebrow">Экран ${screen.number}</span><h1>${escapeHTML(screen.title)}</h1></div></section><div id="partners-screen-content"></div>`;
  const content = root.querySelector("#partners-screen-content");
  if (state.me.entity_type === "edu_institution") await renderITCompanies(content, true);
  else await renderPartnerDirectory(content, true);
}

// Доступная форма объясняется своим названием. У недоступной причина остаётся:
// иначе кнопка «Недоступно» не подсказывает, что сделать (снимок на 1 мая,
// серверный шаблон АНО АЦ).
function reportLink(href, label, description, enabled) {
  const reason = enabled || !description ? "" : `<p class="report-format-reason">${escapeHTML(description)}</p>`;
  return `<article class="report-format-card${enabled ? "" : " disabled"}"><div class="report-format-icon">${navigationIcon("reports")}</div><div><h3>${escapeHTML(label)}</h3>${reason}</div><a class="btn secondary${enabled ? "" : " disabled"}" ${enabled ? `href="${escapeHTML(href)}"` : 'aria-disabled="true"'}>${enabled ? "Сформировать" : "Недоступно"}</a></article>`;
}

// Контрольные даты приходят с сервера: он считает их по московским датам
// (ADR-02), поэтому остаток дней не зависит от часового пояса браузера.
function regulatoryTimelineMarkup(milestones) {
  const rows = (milestones || []).map((milestone) => {
    const days = Number(milestone.days_left);
    const status = milestone.overdue ? "red" : days <= 14 ? "yellow" : "green";
    const left = milestone.overdue ? `просрочено на ${Math.abs(days)} дн.` : days === 0 ? "сегодня" : `${days} календ. дн.`;
    const date = String(milestone.date || "").split("-").reverse().join(".");
    return `<div><span class="risk-dot ${status}"></span><span title="${escapeHTML(milestone.basis || "")}">${escapeHTML(milestone.label)} · ${escapeHTML(date)}</span><b>${escapeHTML(left)}</b></div>`;
  }).join("");
  return `<div class="card"><h2>Регламентный календарь приказа № 270</h2><div class="readiness-list">${rows || '<p class="muted">Контрольные даты недоступны.</p>'}</div></div>`;
}

// Строка активных действий (экран 1): то, что требует шага прямо сейчас — задачи
// в работе, записи с замечаниями, ближайший регламентный срок — и быстрые
// переходы к вводу данных. Числа берутся из тех же ответов сервера, что и
// остальной экран, и сами ничего не считают.
function activeActionsMarkup({ tasks, attention, milestones, quickScreens }) {
  const items = [];
  const open = Array.isArray(tasks) ? tasks : [];
  if (open.length) {
    const escalated = open.filter((task) => task.unassigned_escalated).length;
    items.push(`<div class="action-chip ${escalated ? "warn" : ""}"><span class="action-count">${open.length}</span><span>Задач в работе${escalated ? `, из них эскалировано: ${escalated}` : ""}</span></div>`);
  }
  if (attention > 0) {
    items.push(`<div class="action-chip warn"><span class="action-count">${attention}</span><span>Мероприятий требуют внимания</span></div>`);
  }
  const overdue = (milestones || []).filter((milestone) => milestone.overdue);
  const upcoming = (milestones || []).find((milestone) => !milestone.overdue);
  if (overdue.length) {
    items.push(`<div class="action-chip bad"><span class="action-count">${overdue.length}</span><span>Просроченных контрольных сроков</span></div>`);
  }
  if (upcoming) {
    const days = Number(upcoming.days_left);
    const date = String(upcoming.date || "").split("-").reverse().join(".");
    items.push(`<div class="action-chip ${days <= 14 ? "warn" : ""}"><span class="action-count">${days === 0 ? "0" : days}</span><span>дн. до срока: ${escapeHTML(upcoming.label)} · ${escapeHTML(date)}</span></div>`);
  }
  if (!items.length) items.push(`<div class="action-chip good"><span>Срочных действий нет</span></div>`);
  const quick = (quickScreens || []).map((screen) => `<button class="btn secondary" type="button" data-quick-view="${escapeHTML(screen.id)}">${escapeHTML(screen.label)}</button>`).join("");
  return `<section class="dashboard-actions card" aria-label="Активные действия"><div class="dashboard-actions-row">${items.join("")}</div>${quick ? `<div class="dashboard-quick"><span>Внести данные</span>${quick}</div>` : ""}</section>`;
}

async function renderReportsScreen(root) {
  const screen = CyberCalcScreens.get("reports");
  root.innerHTML = `<section class="page-heading screen-heading"><div><span class="eyebrow">Экран ${screen.number}</span><h1>${escapeHTML(screen.title)}</h1></div><span class="year-badge">${state.year}</span></section><div class="card loading-state"><span class="spinner"></span>Загрузка отчётного контура…</div>`;
  if (!state.categories.length) {
    state.categories = (await api("/categories")) || [];
    state.categories.forEach((category) => (CATEGORY_LABELS[category.code] = category.name));
  }
  const fixedPartner = isEducationReviewer();
  if (fixedPartner) state.partnerID = state.me.partner_id || "";
  let partner = state.partners.find((item) => item.id === state.partnerID);
  if (!partner && state.partners.length === 1) {
    partner = state.partners[0];
    state.partnerID = partner.id;
  }
  if (partner && state.agreementPartnerID !== partner.id) {
    state.agreements = await api(`/agreements?partner_id=${encodeURIComponent(partner.id)}`);
    state.agreementPartnerID = partner.id;
    state.agreementID = "";
  }
  if (!partner) {
    state.agreements = [];
    state.agreementID = "";
    state.agreementPartnerID = "";
  }
  if (!state.agreements.some((agreement) => agreement.id === state.agreementID)) {
    state.agreementID = state.agreements.find((agreement) => agreementIsUsable(agreement))?.id || state.agreements[0]?.id || "";
  }
  const agreement = state.agreements.find((item) => item.id === state.agreementID);
  const availableCategories = state.categories.filter((category) =>
    (!partner || category.audience_scope.includes(partner.partner_kind)) &&
    (!agreement?.activity_codes || agreement.activity_codes.includes(category.code)),
  );
  if (state.reportCategory && !availableCategories.some((category) => category.code === state.reportCategory)) state.reportCategory = "";
  let workflow = null;
  if (partner && agreement) {
    workflow = await api(`/report-workflow?${new URLSearchParams({ agreement_id: agreement.id, report_year: state.year, period_type: state.period })}`);
  }
  let snapshots = [];
  let generatedReports = [];
  let milestones = [];
  try { milestones = await api(`/report-calendar?report_year=${encodeURIComponent(state.year)}`); } catch (_) { milestones = []; }
  if (state.me.entity_type === "organization" && state.me.it_company_id) {
    try { snapshots = await api("/report-snapshots"); } catch (_) { snapshots = []; }
    try { generatedReports = await api("/reports/generated"); } catch (_) { generatedReports = []; }
  }
  const hasMaySnapshot = snapshots.some((item) => Number(item.report_year) === state.year);
  const approved = workflow?.status === "approved";
  const hasCompanyContext = state.me.entity_type === "organization" && !!state.me.it_company_id;
  const hasPartnerContext = !!partner;
  const hasAgreementContext = !!(partner && agreement);
  const statusLabels = { draft: "Черновик", ready: "На рассмотрении", verified: "Согласовано", approved: "Утверждено" };
  const base = new URLSearchParams({ partner_id: state.partnerID, agreement_id: state.agreementID, period_type: state.period, report_year: state.year });
  const categoryQuery = new URLSearchParams(base);
  if (state.reportCategory) categoryQuery.set("category_code", state.reportCategory);
  const constructorQuery = new URLSearchParams(base);
  if (state.reportRiskFilter) constructorQuery.set("risk_filter", state.reportRiskFilter);
  const planFactQuery = new URLSearchParams(constructorQuery);
  planFactQuery.set("report_type", "plan_fact");
  const planFactCSVQuery = new URLSearchParams(planFactQuery);
  planFactCSVQuery.set("format", "csv");
  const reportHint = !partner
    ? "Можно формировать общие рабочие выгрузки; выберите партнёра для справки об отсутствии, а партнёра и соглашение — для типовых соглашений."
    : !agreement
      ? "Справку об отсутствии можно сформировать по всем соглашениям партнёра без факта; для типового соглашения выберите конкретное соглашение."
    : approved
      ? "Отчёт утверждён — рабочие и регламентные выгрузки доступны."
      : `Текущий статус: ${statusLabels[workflow?.status] || "не определён"}; рабочие выгрузки доступны до финального утверждения.`;

  root.innerHTML = `<section class="page-heading screen-heading"><div><span class="eyebrow">Экран ${screen.number}</span><h1>${escapeHTML(screen.title)}</h1></div><span class="year-badge">${state.year}</span></section>
    <div class="card report-builder"><div class="flex between"><div><h2>Конструктор среза</h2></div><span class="status-badge ${approved ? "active" : "pending"}">${escapeHTML(statusLabels[workflow?.status] || "Контекст не выбран")}</span></div>
      <div class="grid cols-3"><div class="field"><label>Год</label><input id="report-year" type="number" min="2000" max="2100" value="${state.year}"></div><div class="field"><label>Период</label><select id="report-period"><option value="plan" ${state.period === "plan" ? "selected" : ""}>План</option><option value="fact" ${state.period === "fact" ? "selected" : ""}>Факт</option></select></div><div class="field"><label>Вид мероприятия</label><select id="report-category"><option value="">Все виды</option>${availableCategories.map((category) => `<option value="${category.code}" ${category.code === state.reportCategory ? "selected" : ""}>${escapeHTML(category.name)}</option>`).join("")}</select></div><div class="field"><label>Риск для конструктора</label><select id="report-risk"><option value="" ${!state.reportRiskFilter ? "selected" : ""}>Все зоны риска</option><option value="green" ${state.reportRiskFilter === "green" ? "selected" : ""}>🟢 Гарантировано</option><option value="yellow" ${state.reportRiskFilter === "yellow" ? "selected" : ""}>🟡 В процессе</option><option value="red" ${state.reportRiskFilter === "red" ? "selected" : ""}>🔴 В зоне риска</option></select></div>${fixedPartner ? `<div class="field"><label>Партнёр</label><input value="${escapeHTML(partner?.name || "Назначенная организация")}" readonly></div>` : `<div class="field"><label>Партнёр</label><select id="report-partner"><option value="">— Выберите —</option>${state.partners.map((item) => `<option value="${item.id}" ${item.id === state.partnerID ? "selected" : ""}>${escapeHTML(item.name)}</option>`).join("")}</select></div>`}<div class="field"><label>Соглашение</label><select id="report-agreement"><option value="">— Выберите —</option>${state.agreements.map((item) => `<option value="${item.id}" ${item.id === state.agreementID ? "selected" : ""}>${escapeHTML(agreementLabel(item))}</option>`).join("")}</select></div></div><p class="context-status">${escapeHTML(reportHint)}</p>
    </div>
    <div class="report-format-grid">${reportLink(`/api/reports/export?${categoryQuery}`, "Excel по выбранному срезу", "Категория, партнёр, год и план/факт.", true)}${reportLink(`/api/reports/export?${planFactQuery}`, "План–факт–дельта", "Абсолютная и процентная дельта по партнёрам и видам с учётом фильтра риска.", hasCompanyContext)}${reportLink(`/api/reports/export?${planFactCSVQuery}`, "План–факт–дельта CSV", "Тот же конструктор в CSV для быстрой сверки.", hasCompanyContext)}${reportLink(`/api/reports/export?${new URLSearchParams({ ...Object.fromEntries(base), report_type: "annex1" })}`, "Приложение № 1", "Детализированный перечень мероприятий.", hasCompanyContext)}${reportLink(`/api/reports/export?${new URLSearchParams({ ...Object.fromEntries(base), report_type: "annex2" })}`, "Приложение № 2", "Реестр стажировок, часов и трудовых договоров.", hasCompanyContext)}${reportLink(`/api/reports/export?${new URLSearchParams({ ...Object.fromEntries(base), report_type: "annex2", mode: "mentors" })}`, "Отчёт по наставникам", "Часы сопровождения и закреплённые стажёры по каждому наставнику.", hasCompanyContext)}${reportLink(`/api/reports/export?${new URLSearchParams({ ...Object.fromEntries(base), report_type: "annex3" })}`, "Приложение № 3", "DOCX-справка по одному соглашению или всем соглашениям партнёра без факта.", hasCompanyContext && hasPartnerContext)}${reportLink(`/api/reports/export?${new URLSearchParams({ ...Object.fromEntries(base), report_type: "annex5" })}`, "Приложение № 5", "Сводный отчёт и выполнение норматива 3%.", hasCompanyContext)}${reportLink(`/api/reports/export?${new URLSearchParams({ report_type: "annex4", report_year: state.year, ...(state.partnerID ? { partner_id: state.partnerID } : {}) })}`, "Приложение № 4", hasMaySnapshot ? "13 граф формы приказа № 270; факт читается из неизменяемого снимка на 1 мая." : "Сначала сформируйте снимок на 1 мая в настройках.", hasCompanyContext && hasMaySnapshot)}${reportLink(`/api/reports/export?${new URLSearchParams({ ...Object.fromEntries(base), report_type: "agreement2" })}`, "Типовое соглашение № 2", "Автозаполнение реквизитов сторон в DOCX.", hasCompanyContext && hasAgreementContext)}${reportLink(`/api/reports/export?${new URLSearchParams({ ...Object.fromEntries(base), report_type: "agreement3" })}`, "Типовое соглашение № 3", "Автозаполнение реквизитов сторон в DOCX.", hasCompanyContext && hasAgreementContext)}${reportLink("", "Формы АНО АЦ", "Комплект ТОП-ИТ/ТОП-ИИ на шести листах требует серверного шаблона.", false)}</div>
    <div class="card"><h2>Реестр сформированных файлов</h2>${generatedReports.length ? `<div class="table-wrap"><table><thead><tr><th>Дата</th><th>Форма</th><th>Год</th><th>Файл</th><th>SHA-256</th><th>Размер</th><th></th></tr></thead><tbody>${generatedReports.map((item) => `<tr><td>${new Date(item.generated_at).toLocaleString("ru-RU")}</td><td>${escapeHTML(item.report_type)}</td><td>${item.report_year}</td><td>${escapeHTML(item.file_name)}</td><td><code>${escapeHTML(item.content_sha256.slice(0, 16))}…</code></td><td>${Number(item.size_bytes).toLocaleString("ru-RU")} Б</td><td><a class="btn secondary" href="/api/reports/generated/${encodeURIComponent(item.id)}">Скачать</a></td></tr>`).join("")}</tbody></table></div>` : '<p class="muted">Файлы ещё не формировались.</p>'}</div>
    ${regulatoryTimelineMarkup(milestones)}
    <div class="grid cols-2"><div class="card"><h2>Комплектность и согласование</h2>${workflow ? `<div class="readiness-list">${workflow.automatic_checks.map((check) => `<div><span class="risk-dot ${check.complete ? "green" : "red"}"></span><span>${escapeHTML(check.label)}</span><b>${check.complete ? "Готово" : "Не выполнено"}</b></div>`).join("")}</div>${workflow.missing.length ? `<p class="error">Не выполнено: ${workflow.missing.map(escapeHTML).join("; ")}</p>` : '<p class="notice">Автоматические проверки пройдены.</p>'}` : '<p class="muted">После выбора соглашения здесь появится готовность комплекта.</p>'}</div><div class="card"><h2>Обменные пакеты .pkg</h2><div class="field"><label>Пакет для сверки</label><input type="file" accept=".pkg" disabled></div><div class="flex"><button class="btn" disabled>Сформировать .pkg</button><button class="btn secondary" disabled>Сверить пакет</button></div></div></div>`;

  const rerender = () => renderReportsScreen(root).catch((error) => showToast(error.message));
  root.querySelector("#report-year").onchange = (event) => { const year = Number(event.target.value); if (!validYear(year)) return event.target.reportValidity(); state.year = year; rerender(); };
  root.querySelector("#report-period").onchange = (event) => { state.period = event.target.value; rerender(); };
  root.querySelector("#report-category").onchange = (event) => { state.reportCategory = event.target.value; rerender(); };
  root.querySelector("#report-risk").onchange = (event) => { state.reportRiskFilter = event.target.value; rerender(); };
  root.querySelector("#report-partner")?.addEventListener("change", (event) => { state.partnerID = event.target.value; state.agreementID = ""; state.agreementPartnerID = ""; rerender(); });
  root.querySelector("#report-agreement").onchange = (event) => { state.agreementID = event.target.value; rerender(); };
}

async function renderSettingsOverview(box) {
  const flags = globalThis.CyberCalcFeatures?.snapshot?.() || {};
  const enabledFlags = Object.values(flags).filter(Boolean).length;
  let snapshots = [];
  if (state.me.entity_type === "organization" && state.me.it_company_id) {
    try { snapshots = await api("/report-snapshots"); } catch (_) { snapshots = []; }
  }
  const canSealSnapshot = ["super_admin", "holding_admin", "org_admin"].includes(state.me.role) && state.me.entity_type === "organization" && state.me.it_company_id;
  const canDownloadSnapshot = ["super_admin", "holding_admin", "org_admin", "auditor_viewer"].includes(state.me.role);
  box.innerHTML = `<div class="grid cols-2"><div class="card"><h2>Контекст экземпляра</h2><div class="settings-facts"><div><span>Режим</span><b>${state.me.entity_type === "organization" ? "ИТ-организация" : "Образовательная организация"}</b></div><div><span>Отчётный год</span><b>${state.year}</b></div><div><span>Организация</span><b>${escapeHTML(state.me.organization_name || state.me.entity_name || "Не назначена")}</b></div><div><span>Функциональные флаги</span><b>${enabledFlags} включено</b></div></div>${state.me.entity_type === "organization" && isStaffUser() ? '<button class="btn" id="settings-target">Настроить целевую сумму 3%</button>' : ""}</div><div class="card"><h2>Защита профиля</h2><div class="settings-facts"><div><span>Роль</span><b>${escapeHTML(valueLabel(state.me.role))}</b></div><div><span>Двухфакторная защита</span><b>${state.me.mfa_enabled ? "Включена" : "Не включена"}</b></div><div><span>Соединение</span><b>Защищено</b></div></div><div class="flex"><button class="btn secondary" id="settings-password">Изменить пароль</button>${state.me.mfa_available && !state.me.mfa_enabled ? '<button class="btn" id="settings-mfa">Включить 2FA</button>' : ""}</div></div><div class="card"><h2>Договоры группы лиц</h2><p>Договоров взаимодействия: <b>${Number(state.legalEntityGroups?.length || 0)}</b>.</p></div><div class="card"><h2>Снимки на 1 мая</h2><p>${snapshots.length ? `Зафиксировано снимков: <b>${snapshots.length}</b>. Последний: ${escapeHTML(snapshots[0].snapshot_date)}.` : "Неизменяемых снимков пока нет."}</p><div class="flex">${canSealSnapshot ? '<button class="btn secondary" id="settings-snapshot">Сформировать снимок</button>' : ""}${snapshots[0] && canDownloadSnapshot ? `<a class="btn secondary" href="/api/report-snapshots/${encodeURIComponent(snapshots[0].id)}">Скачать последний</a>` : ""}<button class="btn secondary" disabled>Проверить криптомодуль</button></div></div></div>`;
  box.querySelector("#settings-target")?.addEventListener("click", (event) => openBudgetTargetDialog(event.currentTarget));
  box.querySelector("#settings-password").onclick = openPasswordDialog;
  box.querySelector("#settings-mfa")?.addEventListener("click", () => app.replaceChildren(renderMFASetup()));
  box.querySelector("#settings-snapshot")?.addEventListener("click", async (event) => {
    event.currentTarget.disabled = true;
    try {
      await api(`/report-snapshots?report_year=${state.year}`, { method: "POST" });
      showToast("Неизменяемый снимок сформирован", "success");
      await renderSettingsOverview(box);
    } catch (error) {
      showToast(error.message);
      event.currentTarget.disabled = false;
    }
  });
}

async function renderSettingsScreen(root) {
  const screen = CyberCalcScreens.get("settings");
  const isAdmin = state.me.role === "super_admin";
  const tabs = [{ id: "context", label: "Контекст и безопасность" }, ...(isAdmin ? [{ id: "users", label: "Пользователи и доступ" }, { id: "okz", label: "Классификатор ОКЗ" }, { id: "settings", label: "Хранение" }, { id: "logs", label: "Audit Trail" }] : [])];
  if (!tabs.some((tab) => tab.id === state.settingsTab)) state.settingsTab = "context";
  root.innerHTML = `<section class="page-heading screen-heading"><div><span class="eyebrow">Экран ${screen.number}</span><h1>${escapeHTML(screen.title)}</h1></div></section><div class="admin-layout settings-layout"><nav class="admin-nav" aria-label="Разделы настроек">${tabs.map((tab) => `<button data-settings-tab="${tab.id}" class="${tab.id === state.settingsTab ? "active" : ""}"><b>${escapeHTML(tab.label)}</b></button>`).join("")}</nav><div id="settings-content"></div></div>`;
  const content = root.querySelector("#settings-content");
  const show = async (tab) => {
    state.settingsTab = tab;
    root.querySelectorAll("[data-settings-tab]").forEach((button) => button.classList.toggle("active", button.dataset.settingsTab === tab));
    if (tab === "context") return renderSettingsOverview(content);
    await renderAdminTab(content, tab);
  };
  root.querySelectorAll("[data-settings-tab]").forEach((button) => { button.onclick = () => show(button.dataset.settingsTab).catch((error) => showToast(error.message)); });
  await show(state.settingsTab);
}

// ------------------------------------------------------------- DASHBOARD --

async function renderDashboard(root) {
  root.appendChild(el(`<div class="muted">Загрузка дашборда…</div>`));
  const fixedEducationPartner = isEducationReviewer();
  if (fixedEducationPartner) state.partnerID = state.me.partner_id || "";
  let d;
  try {
    d = await api(
      `/dashboard?${new URLSearchParams({ report_year: state.year, partner_id: state.partnerID, category_code: state.dashboardCategory, audience: state.dashboardAudience, hide_zero: state.dashboardHideZero ? "1" : "" })}`,
    );
  } catch (e) {
    root.innerHTML = `<div class="error">${escapeHTML(e.message)}</div>`;
    return;
  }
  state.dashboard = d;
  // Календарь и задачи — дополнение экрана: их недоступность не должна лишать
  // пользователя самого дашборда.
  let milestones = [];
  try { milestones = await api(`/report-calendar?report_year=${encodeURIComponent(state.year)}`); } catch (_) { milestones = []; }
  let openTasks = [];
  try { openTasks = await api("/workflow-tasks?status=open"); } catch (_) { openTasks = []; }
  const quickScreens = (CyberCalcScreens.available?.() || []).filter((screen) => CyberCalcScreens.activity?.(screen.id) && screen.id !== "dashboard");

  const groupedChart = (plan, fact) => {
    const counts = (items) => (items || []).reduce((map, item) => map.set(item.category_code, (map.get(item.category_code) || 0) + Number(item.entry_count || 0)), new Map());
    const planMap = counts(plan);
    const factMap = counts(fact);
    const codes = [...new Set([...planMap.keys(), ...factMap.keys()])];
    if (!codes.length)
      return `<div class="chart-empty">Добавьте записи плана или факта — здесь появится сравнение.</div>`;
    const max = Math.max(
      1,
      ...codes.flatMap((code) => [
        planMap.get(code) || 0,
        factMap.get(code) || 0,
      ]),
    );
    return `<div class="compare-chart">
      <div class="chart-legend"><span><i class="legend-plan"></i>План</span><span><i class="legend-fact"></i>Факт</span></div>
      ${codes
        .map((code) => {
          const planCount = planMap.get(code) || 0;
          const factCount = factMap.get(code) || 0;
          return `<div class="compare-row">
            <div class="compare-label" title="${escapeHTML(CATEGORY_LABELS[code] || code)}">${escapeHTML(CATEGORY_LABELS[code] || code)}</div>
            <div class="compare-bars">
              <div class="compare-bar plan" style="width:${(planCount / max) * 100}%"><span>${planCount}</span></div>
              <div class="compare-bar fact" style="width:${(factCount / max) * 100}%"><span>${factCount}</span></div>
            </div>
          </div>`;
        })
        .join("")}
    </div>`;
  };

  const donutChart = (items, title) => {
    const total = (items || []).reduce((sum, item) => sum + Number(item.entry_count || 0), 0);
    if (!items || !items.length || !total) {
      return `<div class="donut-panel"><div class="chart-empty">Нет данных для диаграммы «${escapeHTML(title)}»</div></div>`;
    }
    const grouped = [...(items || []).reduce((map, item) => {
      const current = map.get(item.category_code) || { ...item, entry_count: 0 };
      current.entry_count += Number(item.entry_count || 0);
      map.set(item.category_code, current);
      return map;
    }, new Map()).values()];
    let cursor = 0;
    const segments = grouped.map((item, index) => {
      const start = cursor;
      cursor += clampPercent((item.entry_count / total) * 100);
      return `${CHART_COLORS[index % CHART_COLORS.length]} ${start}% ${cursor}%`;
    });
    return `<div class="donut-panel">
      <div class="donut" style="background:conic-gradient(${segments.join(",")})">
        <div class="donut-hole"><span>${escapeHTML(title)}</span><strong>${total}</strong></div>
      </div>
      <div class="donut-legend">${grouped
        .map(
          (item, index) =>
            `<div><i style="background:${CHART_COLORS[index % CHART_COLORS.length]}"></i><span>${escapeHTML(
              CATEGORY_LABELS[item.category_code] || item.category_code,
            )}</span><b>${Math.round((item.entry_count / total) * 100)}%</b></div>`,
        )
        .join("")}</div>
    </div>`;
  };

  const countsByCategory = (items) => (items || []).reduce((result, item) => {
    result[item.category_code] = (result[item.category_code] || 0) + Number(item.entry_count || 0);
    return result;
  }, {});
  const planCounts = countsByCategory(d.plan_by_category);
  const factCounts = countsByCategory(d.fact_by_category);
  const dashboardSlice = ["plan", "fact", "delta"].includes(state.dashboardSlice)
    ? state.dashboardSlice
    : "fact";
  const sliceLabels = {
    plan: "План",
    fact: "Факт",
    delta: "Дельта (Факт-План)",
  };
  const activityRows = state.categories.map((category) => {
    const plan = planCounts[category.code] || 0;
    const fact = factCounts[category.code] || 0;
    const delta = fact - plan;
    const risk = fact > 0 && (plan === 0 || fact >= plan)
      ? "green"
      : plan > 0 || fact > 0
        ? "yellow"
        : "red";
    const selected = dashboardSlice === "plan" ? plan : dashboardSlice === "delta" ? delta : fact;
    return { ...category, plan, fact, delta, selected, risk };
  });
  const riskCounts = activityRows.reduce((counts, item) => {
    counts[item.risk] += 1;
    return counts;
  }, { green: 0, yellow: 0, red: 0 });
  const riskBuckets = d.risk_buckets || {
    green: { entry_count: riskCounts.green, amount_rub: 0 },
    yellow: { entry_count: riskCounts.yellow, amount_rub: 0 },
    red: { entry_count: riskCounts.red, amount_rub: 0 },
  };
  const factEntries = (d.fact_by_category || []).reduce((sum, item) => sum + Number(item.entry_count || 0), 0);
  const activeAgreements = state.partners.reduce((sum, partner) => sum + Number(partner.active_agreements_count || 0), 0);
  const attentionEntries = Number(riskBuckets.yellow?.entry_count || 0) + Number(riskBuckets.red?.entry_count || 0);
  const selectedPartner = state.partners.find(
    (partner) => partner.id === state.partnerID,
  );
  const partnerFilter = fixedEducationPartner
    ? `<div class="field"><label>Учебное заведение</label><input value="${escapeHTML(selectedPartner?.name || "Назначенное учебное заведение")}" readonly></div>`
    : `<div class="field"><label for="dash-partner">Учебное заведение</label><select id="dash-partner"><option value="">Все учебные заведения</option>${state.partners.map((p) => `<option value="${p.id}" ${p.id === state.partnerID ? "selected" : ""}>${escapeHTML(p.name)}</option>`).join("")}</select></div>`;

  root.innerHTML = `
    <section class="page-heading">
      <div><span class="eyebrow">Экран 1</span><h1>Пульс проекта</h1></div>
      <span class="year-badge">${state.year}</span>
    </section>
    ${activeActionsMarkup({ tasks: openTasks, attention: attentionEntries, milestones, quickScreens })}
    <div class="dashboard-primary-kpis" aria-label="Ключевые показатели">
      <article class="dashboard-kpi target">
        <div class="dashboard-kpi-icon">${dashboardKPIIcon("target")}</div>
        <div class="dashboard-kpi-label">Партнёры</div>
        <div class="dashboard-kpi-value">${state.partners.length}</div>
      </article>
      <article class="dashboard-kpi confirmed">
        <div class="dashboard-kpi-icon">${dashboardKPIIcon("confirmed")}</div>
        <div class="dashboard-kpi-label">Действующие соглашения</div>
        <div class="dashboard-kpi-value">${activeAgreements}</div>
      </article>
      <article class="dashboard-kpi gap reached">
        <div class="dashboard-kpi-icon">${dashboardKPIIcon("confirmed")}</div>
        <div class="dashboard-kpi-label">Мероприятия по факту</div>
        <div class="dashboard-kpi-value">${factEntries}</div>
      </article>
      <article class="dashboard-kpi date">
        <div class="dashboard-kpi-icon">${dashboardKPIIcon("gap")}</div>
        <div class="dashboard-kpi-label">Требуют внимания</div>
        <div class="dashboard-kpi-value">${attentionEntries}</div>
      </article>
    </div>
    ${partnerDistributionMarkup(state.partners)}
    <div class="card dashboard-filter-card">
      <div class="dashboard-toolbar">
        <h2 style="margin:0">Аналитика</h2>
        <div class="dashboard-filters">
          <div class="field"><label for="dash-year">Год</label><input type="number" id="dash-year" min="2000" max="2100" step="1" value="${state.year}"></div>
          ${partnerFilter}
          <div class="field"><label for="dash-category">Вид активности</label><select id="dash-category"><option value="">Все активности</option>${state.categories.map((category) => `<option value="${escapeHTML(category.code)}" ${category.code === state.dashboardCategory ? "selected" : ""}>${escapeHTML(category.name)}</option>`).join("")}</select></div>
          <div class="field"><label for="dash-slice">Срез</label><select id="dash-slice"><option value="plan" ${dashboardSlice === "plan" ? "selected" : ""}>План</option><option value="fact" ${dashboardSlice === "fact" ? "selected" : ""}>Факт</option><option value="delta" ${dashboardSlice === "delta" ? "selected" : ""}>Дельта</option></select></div>
          <div class="field"><label class="check-row" for="dash-hide-zero"><input type="checkbox" id="dash-hide-zero" ${state.dashboardHideZero ? "checked" : ""}> Скрыть нулевые позиции</label></div>
          <div class="field"><label for="dash-audience">Аудитория</label><select id="dash-audience"><option value="">Все аудитории</option>${Object.entries(AUDIENCE_LABELS).map(([code, label]) => `<option value="${code}" ${code === state.dashboardAudience ? "selected" : ""}>${escapeHTML(label)}</option>`).join("")}</select></div>
        </div>
      </div>
    </div>
    <div class="grid cols-2 dashboard-risk-grid">
      <div class="card"><h2>Распределение по готовности</h2><div class="risk-buckets"><div class="green"><span>${Number(riskBuckets.green?.entry_count || 0)}</span><b>Готово</b></div><div class="yellow"><span>${Number(riskBuckets.yellow?.entry_count || 0)}</span><b>В работе</b></div><div class="red"><span>${Number(riskBuckets.red?.entry_count || 0)}</span><b>Требует внимания</b></div></div></div>
      <div class="card"><h2>Все виды мероприятий — ${escapeHTML(sliceLabels[dashboardSlice])}</h2><div class="table-wrap"><table><thead><tr><th>Вид</th><th>${escapeHTML(sliceLabels[dashboardSlice])}</th><th>План</th><th>Факт</th><th>Готовность</th></tr></thead><tbody>${activityRows.map((item) => `<tr><td>${escapeHTML(item.name)}</td><td>${item.selected}</td><td>${item.plan}</td><td>${item.fact}</td><td><span class="risk-label ${item.risk}"><i></i>${item.risk === "green" ? "Готово" : item.risk === "yellow" ? "В работе" : "Нет данных"}</span></td></tr>`).join("")}</tbody></table></div></div>
    </div>
    ${regulatoryTimelineMarkup(milestones)}
    <div class="card"><h2>Количество мероприятий по категориям</h2>${groupedChart(d.plan_by_category, d.fact_by_category)}</div>
    <div class="grid cols-2">
      <div class="card"><h2>Структура плана</h2>${donutChart(d.plan_by_category, "План")}</div>
      <div class="card"><h2>Структура факта</h2>${donutChart(d.fact_by_category, "Факт")}</div>
    </div>
  `;
  root.querySelectorAll("[data-quick-view]").forEach((button) => {
    button.onclick = () => CyberCalcRouter.activate(button.dataset.quickView);
  });
  root.querySelector("#dash-year").onchange = (e) => {
    const year = Number(e.target.value);
    if (validYear(year)) {
      state.year = year;
      render();
    } else {
      e.target.reportValidity();
    }
  };
  const dashboardPartner = root.querySelector("#dash-partner");
  if (dashboardPartner)
    dashboardPartner.onchange = (e) => {
      state.partnerID = e.target.value;
      root.innerHTML = "";
      renderDashboard(root);
    };
  root.querySelector("#dash-category").onchange = (event) => {
    state.dashboardCategory = event.target.value;
    renderDashboard(root);
  };
  root.querySelector("#dash-slice").onchange = (event) => {
    state.dashboardSlice = event.target.value;
    renderDashboard(root);
  };
  root.querySelector("#dash-audience").onchange = (event) => {
    state.dashboardAudience = event.target.value;
    renderDashboard(root);
  };
  root.querySelector("#dash-hide-zero").onchange = (event) => {
    state.dashboardHideZero = event.target.checked;
    renderDashboard(root);
  };
}

async function openBudgetTargetDialog(button) {
  button.disabled = true;
  try {
    const dashboard = await api(
      `/dashboard?${new URLSearchParams({ report_year: state.year })}`,
    );
    const baseValue = prompt(
      `База экономии на льготах за ${state.year - 2} год, руб.:`,
      dashboard.savings_base_rub || "",
    );
    if (baseValue == null) return;
    const base = Number(String(baseValue).replace(",", "."));
    if (!Number.isFinite(base) || base <= 0) {
      alert("Введите положительную базу экономии.");
      return;
    }
    const amount = Math.round(base * 3) / 100;
    const source = prompt("Источник подтверждения ФНС / Минцифры:", dashboard.target_source_reference || "")?.trim();
    if (!source) return;
    const notifiedAt = prompt("Дата доведения Минцифры (ГГГГ-ММ-ДД):", dashboard.target_notified_at || `${state.year}-07-31`)?.trim();
    if (!/^\d{4}-\d{2}-\d{2}$/.test(notifiedAt || "")) { alert("Укажите дату в формате ГГГГ-ММ-ДД."); return; }
    await api("/dashboard/target", {
      method: "POST",
      body: JSON.stringify({ report_year: state.year, savings_base_rub: base, target_amount_rub: amount, source_reference: source, notified_at: notifiedAt }),
    });
    showToast("Целевая сумма сохранена", "success");
  } catch (error) {
    showToast(error.message);
  } finally {
    button.disabled = false;
  }
}

// --------------------------------------------------------------- ENTRIES --

async function renderEntries(root, screen = CyberCalcScreens.activity(state.view)) {
  try {
    await renderPartnerEntries(root, screen);
  } catch (e) {
    root.innerHTML = `<div class="card error">${escapeHTML(e.message)}</div>`;
  }
}

function partnerName(id) {
  if (!id) return "—";
  const p = state.partners.find((p) => p.id === id);
  return p ? p.name : id;
}

function currentCategory() {
  return state.categories.find((c) => c.code === state.categoryCode);
}

function fieldInput(f, value, audience) {
  const val = value === undefined || value === null ? "" : value;
  const partners = Array.isArray(state.partners) ? state.partners : [];
  if (f.type === "select") {
    const opts =
      f.key === "org_name"
        ? partners
            .filter((p) => !audience || p.partner_kind === audience)
            .map((p) => ({ v: p.id, l: p.name }))
        : f.key === "mentor_id"
          ? state.mentors.map((m) => ({ v: m.id, l: m.full_name }))
          : f.key === "staff_member_id"
            ? (state.staffMembers || []).map((m) => ({
                v: m.id,
                l: `${m.fio} · ${m.company_position} · ОКЗ ${m.okz_code}${m.eligible ? " · подтверждён" : " · требует подтверждения"}`,
              }))
          : (f.options || []).map((o) => ({
              v: o,
              l:
                {
                  rpd: "РПД",
                  oop: "ООП",
                  vo: "Высшее образование",
                  spo: "Среднее профессиональное",
                  bachelor: "Бакалавриат",
                  master: "Магистратура",
                  specialist: "Специалитет",
                  absent: "Отсутствует",
                  full_or_partial: "Есть полностью или частично",
                  fixed_term: "Срочный трудовой договор",
                  other: "Другой тип договора",
                  president_instruction: "Поручение Президента РФ",
                  government_instruction: "Поручение Правительства РФ",
                  curator_instruction: "Поручение куратора Министерства",
                  security_council_decision: "Решение Совета Безопасности РФ",
                  president: "Президент РФ",
                  prime_minister: "Председатель Правительства РФ",
                  deputy_prime_minister: "Куратор Министерства",
                  security_council: "Совет Безопасности РФ",
                  development: "Разработка",
                  update: "Актуализация",
                  expertise: "Экспертиза",
                }[o] || o,
            }));
    return `<select data-key="${escapeHTML(f.key)}" data-kind="select" ${f.required ? "required" : ""}>
      <option value="">—</option>
      ${opts
        .map(
          (o) =>
            `<option value="${escapeHTML(o.v)}" ${o.v === val ? "selected" : ""}>${escapeHTML(o.l)}</option>`,
        )
        .join("")}
    </select>`;
  }
  if (f.type === "number") {
    return `<input type="number" min="${f.minimum ?? 0}" max="${f.maximum ?? 1000000000000}" step="${f.integer ? "1" : "any"}" data-key="${escapeHTML(
      f.key,
    )}" data-kind="number" value="${escapeHTML(val)}" ${f.required ? "required" : ""}>`;
  }
  if (f.type === "date") {
    return `<input type="date" data-key="${escapeHTML(f.key)}" data-kind="date" value="${escapeHTML(val)}" ${f.required ? "required" : ""}>`;
  }
  return `<input type="text" maxlength="${f.max_length || 1000}" data-key="${escapeHTML(f.key)}" data-kind="text" value="${escapeHTML(
    val,
  )}" ${f.required ? "required" : ""}>`;
}

function clearFieldErrors(container) {
  container
    .querySelectorAll(".invalid")
    .forEach((input) => input.classList.remove("invalid"));
  container.querySelectorAll(".field-error").forEach((error) => {
    error.textContent = "";
    error.style.display = "none";
  });
}

function setFieldError(input, message) {
  input.classList.add("invalid");
  const error = input.closest(".field")?.querySelector(".field-error");
  if (error) {
    error.textContent = message;
    error.style.display = "block";
  }
}

function collectAndValidateEntryPayload(fieldsBox, category) {
  clearFieldErrors(fieldsBox);
  const payload = {};
  let firstInvalid = null;

  category.fields.forEach((field) => {
    const input = fieldsBox.querySelector(
      `[data-key="${CSS.escape(field.key)}"]`,
    );
    if (!input) return;
    const raw = input.value.trim();
    let message = "";
    const uiRequired = field.required || (category.code === "teachers" && field.key === "staff_member_id");
    if (!raw && uiRequired) {
      message = "Поле обязательно";
    } else if (raw) {
      if (field.type === "number") {
        const number = Number(raw);
        if (!Number.isFinite(number)) message = "Введите корректное число";
        else if (number < 0) message = "Значение не может быть отрицательным";
        else if (field.minimum !== undefined && number < field.minimum)
          message = `Значение должно быть не меньше ${field.minimum}`;
        else if (field.maximum !== undefined && number > field.maximum)
          message = `Значение должно быть не больше ${field.maximum}`;
        else if (number > 1_000_000_000_000)
          message = "Значение слишком велико";
        else if (field.integer && !Number.isInteger(number))
          message = "Введите целое число";
        else payload[field.key] = number;
      } else {
        const maxLength = field.max_length || 1000;
        if (raw.length > maxLength) message = `Не более ${maxLength} символов`;
        else payload[field.key] = raw;
      }
    }
    if (message) {
      setFieldError(input, message);
      firstInvalid ||= input;
    }
  });

  const requirePositive = (key, message) => {
    const input = fieldsBox.querySelector(`[data-key="${CSS.escape(key)}"]`);
    if (input && Number(payload[key]) <= 0) {
      setFieldError(input, message);
      firstInvalid ||= input;
    }
  };
  if (category.code === "teachers")
    requirePositive(
      "academic_hours",
      "Количество часов должно быть больше нуля",
    );
  if (category.code === "teachers") {
    const ranges = {
      bachelor: [1, 8],
      master: [9, 12],
      specialist: [1, 13],
      spo: [1, 10],
    };
    const range = ranges[payload.education_level];
    const semester = Number(payload.semester);
    if (range && (semester < range[0] || semester > range[1])) {
      const input = fieldsBox.querySelector('[data-key="semester"]');
      if (input) {
        setFieldError(
          input,
          `Для выбранного уровня допустимы семестры ${range[0]}–${range[1]}`,
        );
        firstInvalid ||= input;
      }
    }
  }
  if (["it_clubs", "teacher_training", "edu_content"].includes(category.code)) {
    for (const key of ["budget_funding", "citizen_funding"]) {
      if (payload[key] === "full_or_partial") {
        const input = fieldsBox.querySelector(`[data-key="${key}"]`);
        if (input) {
          setFieldError(
            input,
            key === "budget_funding"
              ? "Бюджетное финансирование полностью или частично запрещено"
              : "Финансирование средствами граждан полностью или частично запрещено",
          );
          firstInvalid ||= input;
        }
      }
    }
  }
  if (
    category.code === "employment_practice" &&
    payload.labor_contract_type === "other"
  ) {
    const input = fieldsBox.querySelector('[data-key="labor_contract_type"]');
    if (input) {
      setFieldError(
        input,
        "Для зачёта практики требуется срочный трудовой договор",
      );
      firstInvalid ||= input;
    }
  }
  if (
    category.code === "internship" ||
    category.code === "employment_practice"
  ) {
    requirePositive(
      "duration_months",
      "Продолжительность должна быть больше нуля",
    );
    if (
      !(
        Number(payload.student_load_hours_per_month) > 0 ||
        Number(payload.mentor_load_hours_per_month) > 0
      )
    ) {
      const input = fieldsBox.querySelector(
        '[data-key="student_load_hours_per_month"]',
      );
      if (input) {
        setFieldError(
          input,
          "Укажите нагрузку студента или наставника больше нуля",
        );
        firstInvalid ||= input;
      }
    }
  }
  if (category.code === "top_it")
    requirePositive("cofinancing_amount_rub", "Сумма должна быть больше нуля");
  if (category.code === "minc_decision")
    requirePositive("amount_manual", "Сумма должна быть больше нуля");
  if (
    category.code === "it_clubs" &&
    !(
      Number(payload.academic_hours) > 0 ||
      Number(payload.developed_programs_count) > 0
    )
  ) {
    const input = fieldsBox.querySelector('[data-key="academic_hours"]');
    if (input) {
      setFieldError(
        input,
        "Укажите часы или количество разработанных программ",
      );
      firstInvalid ||= input;
    }
  }
  if (
    category.code === "teacher_training" &&
    !(
      Number(payload.developed_programs_count) > 0 ||
      (Number(payload.academic_hours_per_teacher) > 0 &&
        Number(payload.trained_teachers_count) > 0)
    )
  ) {
    const input = fieldsBox.querySelector(
      '[data-key="developed_programs_count"]',
    );
    if (input) {
      setFieldError(
        input,
        "Укажите разработанную программу либо часы и число обученных учителей",
      );
      firstInvalid ||= input;
    }
  }
  if (
    category.code === "edu_content" &&
    !(
      Number(payload.student_platform_months) > 0 ||
      Number(payload.teacher_platform_months) > 0
    )
  ) {
    const input = fieldsBox.querySelector(
      '[data-key="student_platform_months"]',
    );
    if (input) {
      setFieldError(input, "Укажите доступ школьников или учителей");
      firstInvalid ||= input;
    }
  }

  if (firstInvalid) firstInvalid.focus();
  return { payload, valid: !firstInvalid };
}

async function openStaffMembersDialog() {
  const role = state.me?.role;
  const canManage = ["super_admin", "holding_admin", "org_admin", "hr_specialist"].includes(role);
  const canConfirm = ["super_admin", "holding_admin", "org_admin"].includes(role);
  const drawer = CyberCalcUI.openDrawer({
    id: "staff-members-drawer",
    title: "Сотрудники-преподаватели",
    mode: canManage ? "edit" : "view",
    wide: true,
    content: `
    ${canManage ? `<form id="staff-form"><input type="hidden" name="id"><div class="grid cols-3">
      <div class="field"><label>ФИО *</label><input name="fio" maxlength="200" required></div>
      <div class="field"><label>Должность *</label><input name="company_position" maxlength="200" required></div>
      <div class="field"><label>Подразделение</label><input name="company_department" maxlength="200"></div>
      <div class="field"><label>Код ОКЗ *</label><input name="okz_code" pattern="[0-9]{4}" maxlength="4" list="staff-okz" required><datalist id="staff-okz"></datalist></div>
      <div class="field"><label>ИТ-стаж за 5 лет, дней *</label><input name="it_experience_days" type="number" min="0" max="1827" required></div>
      <div class="field"><label>Статус</label><select name="record_status" ${canConfirm ? "" : "disabled"}><option value="unconfirmed_by_admin">Ожидает подтверждения</option><option value="confirmed">Подтверждён</option></select></div>
      <div class="field"><label>Документ о стаже</label><input name="experience_document_reference" maxlength="1000" placeholder="СТД-Р / трудовая книжка, номер и дата"></div>
    </div><div class="flex"><button class="btn" type="submit">Сохранить профиль</button><button class="btn secondary" type="button" data-staff-reset>Новый профиль</button></div><p class="error" data-staff-error></p></form>` : ""}
    <div id="staff-list">Загрузка…</div>
  `,
  });
  const backdrop = drawer.element;
  const form = backdrop.querySelector("#staff-form");
  let items = [];
  const reset = () => {
    if (!form) return;
    form.reset();
    form.elements.id.value = "";
    form.elements.record_status.value = "unconfirmed_by_admin";
  };
  const paint = () => {
    backdrop.querySelector("#staff-list").innerHTML = items.length
      ? `<div class="table-wrap"><table><thead><tr><th>Сотрудник</th><th>Должность</th><th>ОКЗ</th><th>Стаж</th><th>Статус</th><th></th></tr></thead><tbody>${items.map((item) => `<tr><td><b>${escapeHTML(item.fio)}</b><br><small>${escapeHTML(item.company_department || "—")}</small></td><td>${escapeHTML(item.company_position)}</td><td><code>${escapeHTML(item.okz_code)}</code><br><small>${escapeHTML(item.okz_name)}</small></td><td>${item.it_experience_days} дн.</td><td><span class="status-badge ${item.eligible ? "active" : "inactive"}">${item.eligible ? "Подтверждён" : "Не подтверждён"}</span></td><td>${canManage ? `<button class="btn secondary" data-staff-edit="${item.id}">Изменить</button>` : ""}</td></tr>`).join("")}</tbody></table></div>`
      : '<p class="muted">Сотрудники ещё не добавлены.</p>';
    backdrop.querySelectorAll("[data-staff-edit]").forEach((button) => button.onclick = () => {
      const item = items.find((value) => value.id === button.dataset.staffEdit);
      if (!item || !form) return;
      Object.keys(item).forEach((key) => { if (form.elements[key]) form.elements[key].value = item[key] ?? ""; });
      form.elements.id.value = item.id;
      form.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  };
  const reload = async () => { items = await api("/staff-members"); paint(); };
  try {
    const [, okz] = await Promise.all([reload(), canManage ? api("/okz?level=4&limit=100") : Promise.resolve({ items: [] })]);
    if (form) backdrop.querySelector("#staff-okz").innerHTML = okz.items.map((item) => `<option value="${escapeHTML(item.code)}">${escapeHTML(item.name)}</option>`).join("");
  } catch (error) {
    backdrop.querySelector("#staff-list").innerHTML = `<p class="error">${escapeHTML(error.message)}</p>`;
  }
  if (!form) return;
  backdrop.querySelector("[data-staff-reset]").onclick = reset;
  form.onsubmit = async (event) => {
    event.preventDefault();
    const submit = form.querySelector('button[type="submit"]');
    const errorBox = form.querySelector("[data-staff-error]");
    const id = form.elements.id.value;
    const body = Object.fromEntries(new FormData(form));
    delete body.id;
    body.it_experience_days = Number(body.it_experience_days);
    if (!canConfirm) body.record_status = "unconfirmed_by_admin";
    submit.disabled = true;
    errorBox.textContent = "";
    try {
      await api(id ? `/staff-members/${id}` : "/staff-members", { method: id ? "PUT" : "POST", body: JSON.stringify(body) });
      showToast("Профиль сотрудника сохранён", "success");
      reset();
      await reload();
    } catch (error) { errorBox.textContent = error.message; }
    finally { submit.disabled = false; }
  };
}

async function openTeachingPayoutsDialog() {
  const role = state.me?.role;
  const canManage = ["super_admin", "holding_admin", "org_admin", "financial_specialist"].includes(role);
  const teachingEntries = (state.entries || []).filter((item) => item.category_code === "teachers");
  const drawer = CyberCalcUI.openDrawer({
    id: "teaching-payouts-drawer",
    title: "График компенсаций",
    mode: canManage ? "edit" : "view",
    wide: true,
    content: `
    ${canManage ? `<form id="payout-form"><input type="hidden" name="id"><div class="grid cols-3">
      <div class="field"><label>Педагогическая нагрузка *</label><select name="teaching_activity_id" required><option value="">— Выберите —</option>${teachingEntries.map((item) => `<option value="${item.id}">${escapeHTML(item.payload.teacher_full_name || "Преподаватель")} · ${escapeHTML(item.payload.course_name || "Курс")} · ${escapeHTML(item.period_type === "fact" ? "Факт" : "План")}</option>`).join("")}</select></div>
      <div class="field"><label>Квартал *</label><select name="target_quarter" required>${[1,2,3,4].map((q) => `<option value="Q${q}">Q${q}</option>`).join("")}</select></div>
      <div class="field"><label>Финансовый год *</label><input name="target_year" type="number" min="2000" max="2100" value="${state.year}" required></div>
      <div class="field"><label>Плановая компенсация, ₽ *</label><input name="planned_compensation_rub" type="number" min="0" step="0.01" required></div>
      <div class="field"><label>Дата выплаты</label><input name="payout_date" type="date"></div>
      <div class="field"><label>Приказ / платёжный документ</label><input name="payout_order_num" maxlength="200"></div>
      <div class="field"><label>Ссылка на скан</label><input name="payout_scan_file" maxlength="1000"></div>
      <label class="check-row"><input name="is_fully_paid" type="checkbox"> Выплачено полностью</label>
    </div><div class="flex"><button class="btn" type="submit">Сохранить выплату</button><button class="btn secondary" type="button" data-payout-reset>Новая выплата</button></div><p class="error" data-payout-error></p></form>` : ""}
    <div id="payout-list">Загрузка…</div>
  `,
  });
  const backdrop = drawer.element;
  let items = [];
  const form = backdrop.querySelector("#payout-form");
  const reset = () => { if (form) { form.reset(); form.elements.id.value = ""; form.elements.target_year.value = state.year; } };
  const paint = () => {
    backdrop.querySelector("#payout-list").innerHTML = items.length
      ? `<div class="table-wrap"><table><thead><tr><th>Преподаватель / курс</th><th>Период</th><th>План</th><th>Статус</th><th>Документ</th><th></th></tr></thead><tbody>${items.map((item) => `<tr><td><b>${escapeHTML(item.teacher_full_name || "—")}</b><br><small>${escapeHTML(item.course_name || "—")}</small></td><td>${escapeHTML(item.target_quarter)} ${item.target_year}</td><td>${fmtMoney(item.planned_compensation_rub)}</td><td><span class="status-badge ${item.is_fully_paid ? "active" : "inactive"}">${item.is_fully_paid ? "Выплачено" : "Запланировано"}</span>${item.payout_date ? `<br><small>${escapeHTML(item.payout_date)}</small>` : ""}</td><td>${escapeHTML(item.payout_order_num || "—")}</td><td>${canManage ? `<button class="btn secondary" data-payout-edit="${item.id}">Изменить</button>` : ""}</td></tr>`).join("")}</tbody></table></div>`
      : '<p class="muted">Выплаты ещё не запланированы.</p>';
    backdrop.querySelectorAll("[data-payout-edit]").forEach((button) => button.onclick = () => {
      const item = items.find((value) => value.id === button.dataset.payoutEdit);
      if (!item || !form) return;
      ["teaching_activity_id", "target_quarter", "target_year", "planned_compensation_rub", "payout_date", "payout_order_num", "payout_scan_file"].forEach((key) => { form.elements[key].value = item[key] ?? ""; });
      form.elements.is_fully_paid.checked = item.is_fully_paid;
      form.elements.id.value = item.id;
      form.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  };
  const reload = async () => { items = await api(`/teaching-payouts?year=${state.year}`); paint(); };
  try { await reload(); } catch (error) { backdrop.querySelector("#payout-list").innerHTML = `<p class="error">${escapeHTML(error.message)}</p>`; }
  if (!form) return;
  backdrop.querySelector("[data-payout-reset]").onclick = reset;
  form.onsubmit = async (event) => {
    event.preventDefault();
    const id = form.elements.id.value;
    const body = Object.fromEntries(new FormData(form));
    delete body.id;
    body.target_year = Number(body.target_year);
    body.planned_compensation_rub = Number(body.planned_compensation_rub);
    body.is_fully_paid = form.elements.is_fully_paid.checked;
    const submit = form.querySelector('button[type="submit"]');
    const errorBox = form.querySelector("[data-payout-error]");
    submit.disabled = true;
    errorBox.textContent = "";
    try {
      await api(id ? `/teaching-payouts/${id}` : "/teaching-payouts", { method: id ? "PUT" : "POST", body: JSON.stringify(body) });
      showToast("Выплата сохранена", "success");
      reset();
      await reload();
    } catch (error) { errorBox.textContent = error.message; }
    finally { submit.disabled = false; }
  };
}

async function openEntryModal(entry, readOnly = false) {
  const cat = currentCategory();
  if (!cat) {
    alert("Категория не загружена. Обновите страницу и повторите попытку.");
    return;
  }
  const isEdit = !!entry;
  const financialOnly = isEdit && !readOnly && state.me?.role === "financial_specialist" && cat.code === "teachers";
  const attachmentOnly = isEdit && readOnly && state.me?.entity_type === "edu_institution" && state.me?.role === "curator" && cat.code === "internship";
  const attachmentReadOnly = readOnly && !attachmentOnly;
  const partner = state.partners.find(
    (p) => p.id === (entry?.partner_id || state.partnerID),
  );
  if (!partner) {
    showToast("Сначала выберите учебное заведение");
    return;
  }
  const agreementID = entry?.agreement_id || state.agreementID;
  const agreement = state.agreements.find((item) => item.id === agreementID);
  if (!agreement) {
    showToast("Сначала выберите соглашение");
    return;
  }
  if (!isEdit && !agreementIsUsable(agreement)) {
    showToast(
      "Новая запись возможна только по действующему соглашению за выбранный год",
    );
    return;
  }
  try {
    state.mentors = await api(
      `/mentors?partner_id=${encodeURIComponent(partner.id)}`,
    );
    if (cat.code === "teachers") state.staffMembers = await api("/staff-members");
  } catch (e) {
    showToast(e.message);
    return;
  }
  const payload = entry
    ? { ...entry.payload }
    : {
        org_name: partner.id,
        level: partner.partner_kind === "kolledj" ? "spo" : "vo",
      };
  const audience = entry ? entry.audience : partner.partner_kind;
  let savedID = null;
  let busy = false;
  const complianceMarkup = entry?.compliance
    ? `<div class="card compliance-card"><div class="flex between"><h3>Документальная готовность</h3><span class="risk-label ${escapeHTML(entry.compliance.state)}"><i></i>${entry.compliance.state === "green" ? "Готово" : entry.compliance.state === "yellow" ? "Нужна доработка" : "Заблокировано"}</span></div><ul>${entry.compliance.checks.map((check) => `<li>${check.complete ? "✓" : check.blocking ? "✕" : "!"} ${escapeHTML(check.label)}</li>`).join("")}</ul><small>Набор правил: ${escapeHTML(entry.compliance.ruleset_version)}</small></div>`
    : "";

  const drawerTitle = `${readOnly ? "Просмотр записи" : financialOnly ? "Подтверждение компенсации" : isEdit ? "Редактировать запись" : "Новая запись"} — ${cat.name}`;
  const drawer = CyberCalcUI.openDrawer({
    id: "entry-drawer",
    title: drawerTitle,
    mode: readOnly ? "view" : "edit",
    wide: true,
    canClose: () => !busy,
    content: `
    <form id="m-form" novalidate>
    ${financialOnly ? '<p class="notice">Финансовая роль изменяет только квартал, плановую компенсацию и реквизиты выплаты. Учебные показатели и расчётная сумма защищены от изменения.</p>' : ""}
    ${entry?.ministry_card_backfill_status === "manual_review" ? '<p class="notice">Запись перенесена из прежнего формата. Проверьте и заполните карточку Решения: до сохранения она не участвует в зачёте.</p>' : ""}
    <div class="field"><label>Аудитория</label>
      <select id="m-audience" disabled><option value="${escapeHTML(audience)}">${escapeHTML(AUDIENCE_LABELS[audience])}</option></select>
    </div>
    <div class="field"><label>Соглашение</label><input value="${escapeHTML(agreementLabel(agreement))}" disabled></div>
    <div class="field"><label>Метод определения стоимости</label><select id="m-cost-method" ${readOnly || financialOnly ? "disabled" : ""}><option value="average" ${(entry?.cost_method || "average") === "average" ? "selected" : ""}>Средние значения Минцифры</option><option value="actual" ${entry?.cost_method === "actual" ? "selected" : ""} ${state.period === "fact" ? "" : "disabled"}>Фактические затраты</option></select><div class="field-hint">${state.period === "fact" ? "Для фактических затрат при утверждении отчёта потребуются реквизиты аудиторского заключения." : "В плане используется расчёт по методике. Фактически понесённые затраты указываются в отчёте «Факт»."}</div></div>
    <div class="field" id="m-actual-field"><label>Фактическая сумма, руб. *</label><input id="m-actual-amount" type="number" min="0.01" max="99999999999999.99" step="0.01" value="${escapeHTML(entry?.actual_amount_rub || "")}" ${readOnly || financialOnly ? "disabled" : ""}></div>
    <div id="m-fields"></div>
    ${complianceMarkup}
    ${
      isEdit && !readOnly
        ? `<div class="field"><label>Комментарий к изменению (обязателен)</label><textarea id="m-comment" rows="2"></textarea></div>`
        : ""
    }
    <div class="error" id="m-error" style="display:none"></div>
    <div class="flex between" style="margin-top:14px">
      <div>${entry ? `<span class="muted">Сумма в отчёте: ${fmtMoney(entry.amount_rub)} · по методике: ${fmtMoney(entry.formula_amount_rub)}</span>` : ""}</div>
      <div class="flex">
        <button type="button" class="btn secondary" id="m-cancel">${readOnly ? "Закрыть" : "Отмена"}</button>
        ${readOnly ? "" : `<button type="submit" class="btn" id="m-save">${isEdit ? "Сохранить" : "Создать"}</button>`}
      </div>
    </div>
    ${isEdit && cat.code === "minc_decision" ? '<div class="card" id="m-cost-history"><h2>История подтверждённой стоимости</h2><div class="loading-state"><span class="spinner"></span>Загрузка…</div></div>' : ""}
    ${renderAttachSection(attachmentReadOnly)}
    ${isEdit && cat.code === "top_it" ? renderTopItemsSection() : ""}
    </form>
  `,
  });
  const backdrop = drawer.element;

  if (isEdit && cat.code === "minc_decision") {
    api(`/entries/${encodeURIComponent(entry.id)}/cost-history`).then((items) => {
      const history = backdrop.querySelector("#m-cost-history div");
      history.className = "table-wrap";
      history.innerHTML = items.length
        ? `<table><thead><tr><th>Редакция</th><th>Было</th><th>Стало</th><th>Основание</th><th>Причина</th><th>Дата</th></tr></thead><tbody>${items.map((item) => `<tr><td>№ ${item.revision_no}</td><td>${item.previous_amount_rub == null ? "—" : fmtMoney(item.previous_amount_rub)}</td><td>${fmtMoney(item.confirmed_amount_rub)}</td><td>${escapeHTML(item.calculation_basis)}</td><td>${escapeHTML(item.correction_reason)}</td><td>${new Date(item.changed_at).toLocaleString("ru-RU")}</td></tr>`).join("")}</tbody></table>`
        : '<span class="muted">История пока пуста</span>';
    }).catch((error) => {
      const history = backdrop.querySelector("#m-cost-history div");
      history.className = "error";
      history.textContent = error.message;
    });
  }

  const fieldsBox = backdrop.querySelector("#m-fields");
  const syncCostMethod = () => {
    const actual = backdrop.querySelector("#m-cost-method").value === "actual";
    backdrop.querySelector("#m-actual-field").hidden = !actual;
    backdrop.querySelector("#m-actual-amount").required = actual;
  };
  backdrop.querySelector("#m-cost-method").onchange = syncCostMethod;
  syncCostMethod();
  const attachmentUpload = readOnly
    ? null
    : CyberCalcUI.bindUpload(backdrop.querySelector("[data-upload]"), (files) => {
        const prompt = backdrop.querySelector(".ui-dropzone span");
        if (prompt && files.length) prompt.textContent = `Выбрано файлов: ${files.length}`;
      });
  cat.fields.forEach((f) => {
    const row = el(
      `<div class="field"><label>${escapeHTML(f.label)}${f.required || (cat.code === "teachers" && f.key === "staff_member_id") ? " *" : ""}</label><div class="field-error" style="display:none"></div></div>`,
    );
    row.insertBefore(
      el(fieldInput(f, payload[f.key], audience)),
      row.querySelector(".field-error"),
    );
    if (cat.code === "teachers" && f.key === "staff_member_id") row.querySelector("select").required = true;
    if (f.key === "org_name") {
      const hasPartners =
        Array.isArray(state.partners) &&
        state.partners.some((partner) => partner.partner_kind === audience);
      row.appendChild(
        el(
          `<div class="field-hint partner-hint"${hasPartners ? ' style="display:none"' : ""}>Для этой аудитории пока нет партнёров. Попросите администратора добавить организацию.</div>`,
        ),
      );
    }
    fieldsBox.appendChild(row);
    if (f.key === "org_name" || readOnly) row.querySelector("input,select").disabled = true;
    if (f.key === "mentor_full_name") row.hidden = true;
    if (f.key === "mentor_id") {
      const select = row.querySelector("select");
      select.onchange = () => {
        fieldsBox.querySelector('[data-key="mentor_full_name"]').value =
          state.mentors.find((m) => m.id === select.value)?.full_name || "";
      };
      const add = readOnly ? null : el(
        '<button type="button" class="btn secondary">+ Наставник</button>',
      );
      if (add) add.onclick = async () => {
        const name = prompt("Фамилия, имя, отчество наставника (при наличии)");
        if (!name) return;
        add.disabled = true;
        try {
          const mentor = await api("/mentors", {
            method: "POST",
            body: JSON.stringify({ partner_id: partner.id, full_name: name }),
          });
          state.mentors.push(mentor);
          select.append(new Option(mentor.full_name, mentor.id));
          select.value = mentor.id;
          select.onchange();
        } catch (e) {
          showToast(e.message);
        } finally {
          add.disabled = false;
        }
      };
      if (add) row.appendChild(add);
    }
    if (cat.code === "teachers" && ["teacher_full_name", "employee_position", "okz_code", "it_experience_days", "it_experience_reference"].includes(f.key)) {
      row.hidden = true;
    }
    if (f.key === "staff_member_id") {
      const select = row.querySelector("select");
      const hint = el('<div class="field-hint"></div>');
      const syncStaff = () => {
        const staff = (state.staffMembers || []).find((item) => item.id === select.value);
        hint.textContent = staff
          ? `${staff.record_status === "confirmed" ? "✓ Профиль подтверждён" : "⚠ Профиль пока не подтверждён: запись сохранится, но не попадёт в зачёт"}. Стаж: ${staff.it_experience_days} дней; ОКЗ ${staff.okz_code}.`
          : "Сначала добавьте сотрудника в справочник преподавателей.";
        if (!staff) return;
        const values = { teacher_full_name: staff.fio, employee_position: staff.company_position, okz_code: staff.okz_code, it_experience_days: staff.it_experience_days, it_experience_reference: staff.experience_document_reference };
        Object.entries(values).forEach(([key, value]) => {
          const input = fieldsBox.querySelector(`[data-key="${key}"]`);
          if (input) input.value = value ?? "";
        });
      };
      select.addEventListener("change", syncStaff);
      row.appendChild(hint);
      queueMicrotask(syncStaff);
    }
  });

  if (financialOnly) {
    const allowed = new Set(["compensation_quarter", "planned_compensation_rub", "payment_status", "payment_date", "payment_order_reference"]);
    fieldsBox.querySelectorAll("[data-key]").forEach((input) => {
      if (!allowed.has(input.dataset.key)) input.disabled = true;
    });
  }
  const documentType = backdrop.querySelector("#attach-document-type");
  if (documentType) {
    const restrictedTypes = state.me?.role === "financial_specialist"
      ? ["payment_order"]
      : state.me?.role === "hr_specialist"
        ? ["labor_contract", "incoming_certificate", "mentor_order"]
        : state.me?.entity_type === "edu_institution" && state.me?.role === "curator"
          ? ["outgoing_certificate"]
          : null;
    if (restrictedTypes) {
      [...documentType.options].forEach((option) => { option.disabled = !restrictedTypes.includes(option.value); });
      documentType.value = restrictedTypes[0];
    }
  }

  if (cat.code === "teachers") {
    const level = fieldsBox.querySelector('[data-key="education_level"]');
    const semester = fieldsBox.querySelector('[data-key="semester"]');
    const semesterRanges = {
      bachelor: [1, 8],
      master: [9, 12],
      specialist: [1, 13],
      spo: [1, 10],
    };
    const syncSemesterRange = () => {
      const range = semesterRanges[level.value] || [1, 13];
      semester.min = String(range[0]);
      semester.max = String(range[1]);
      semester.placeholder = `${range[0]}–${range[1]}`;
      semester.title = `Допустимые семестры: ${range[0]}–${range[1]}`;
    };
    level.addEventListener("change", syncSemesterRange);
    syncSemesterRange();
  }

  const closeModal = drawer.close;
  backdrop.querySelector("#m-cancel").onclick = closeModal;
  if (!readOnly) backdrop.querySelector("#m-form").onsubmit = async (event) => {
    event.preventDefault();
    if (busy) return;
    const validation = collectAndValidateEntryPayload(fieldsBox, cat);
    if (!validation.valid) return;
    const newPayload = validation.payload;
    const aud = backdrop.querySelector("#m-audience").value;
    const errBox = backdrop.querySelector("#m-error");
    const saveButton = backdrop.querySelector("#m-save");
    errBox.style.display = "none";
    saveButton.disabled = true;
    busy = true;
    saveButton.textContent = isEdit ? "Сохраняем…" : "Создаём…";
    try {
      validateFileBatch(attachmentUpload.files);
      if (!savedID && isEdit) {
        const comment = backdrop.querySelector("#m-comment").value.trim();
        if (!comment)
          throw new Error("Комментарий обязателен при редактировании");
        await api(`/entries/${entry.id}`, {
          method: "PUT",
          body: JSON.stringify({
            payload: newPayload,
            audience: aud,
            agreement_id: agreementID,
            cost_method: backdrop.querySelector("#m-cost-method").value,
            actual_amount_rub: backdrop.querySelector("#m-cost-method").value === "actual" ? Number(backdrop.querySelector("#m-actual-amount").value) : null,
            comment,
          }),
        });
        savedID = entry.id;
      } else if (!savedID) {
        const partnerId = newPayload.org_name || null;
        const created = await api(`/entries`, {
          method: "POST",
          body: JSON.stringify({
            category_code: state.categoryCode,
            partner_id: partnerId,
            agreement_id: agreementID,
            period_type: state.period,
            report_year: state.year,
            audience: aud,
            payload: newPayload,
            cost_method: backdrop.querySelector("#m-cost-method").value,
            actual_amount_rub: backdrop.querySelector("#m-cost-method").value === "actual" ? Number(backdrop.querySelector("#m-actual-amount").value) : null,
          }),
        });
        savedID = created.id;
      }
      fieldsBox
        .querySelectorAll("input,select,button")
        .forEach((input) => (input.disabled = true));
      await uploadFileBatch(
        savedID,
        attachmentUpload.files,
        backdrop.querySelector("#attach-document-type").value,
      );
      busy = false;
      closeModal();
      const root = document.getElementById("content");
      if (root) await renderEntries(root);
    } catch (e) {
      errBox.textContent =
        (savedID
          ? "Запись сохранена. Не удалось загрузить вложения; повторите загрузку кнопкой ниже. "
          : "") + e.message;
      errBox.style.display = "block";
    } finally {
      busy = false;
      saveButton.disabled = false;
      saveButton.textContent = savedID
        ? "Повторить загрузку файлов"
        : isEdit
          ? "Сохранить"
          : "Создать";
    }
  };

  backdrop.querySelector("#m-audience").onchange = (event) => {
    const partnerSelect = fieldsBox.querySelector('[data-key="org_name"]');
    if (!partnerSelect) return;
    const selected = partnerSelect.value;
    const options = (
      Array.isArray(state.partners) ? state.partners : []
    ).filter((partner) => partner.partner_kind === event.target.value);
    partnerSelect.innerHTML = `<option value="">—</option>${options
      .map(
        (partner) =>
          `<option value="${escapeHTML(partner.id)}">${escapeHTML(partner.name)}</option>`,
      )
      .join("")}`;
    const partnerHint = partnerSelect
      .closest(".field")
      ?.querySelector(".partner-hint");
    if (partnerHint)
      partnerHint.style.display = options.length ? "none" : "block";
    if (options.some((partner) => partner.id === selected))
      partnerSelect.value = selected;
    clearFieldErrors(fieldsBox);
  };

  if (isEdit) {
    wireAttachSection(backdrop, entry.id, attachmentReadOnly, attachmentUpload);
    if (cat.code === "top_it") wireTopItems(backdrop, entry.id, !readOnly);
  } else {
    backdrop.querySelector("#attach-list").textContent =
      "Файлы необязательны. Выбранные файлы загрузятся после создания записи.";
    backdrop.querySelector("#attach-upload").hidden = true;
  }
}

function renderAttachSection(readOnly = false) {
  return `<div class="card" style="margin-top:14px;background:transparent;padding:0;border:none">
    <h2>Вложения</h2><p class="muted">${readOnly ? "Документы, приложенные ИТ-организацией к перечню мероприятий." : "Необязательно: до 20 файлов за раз, суммарно до 20 МБ."}</p>
    <div id="attach-list" class="attach-list muted">Загрузка…</div>
    ${readOnly ? "" : `<div class="field" style="margin-top:8px">
      <label>Тип подтверждающего документа</label><select id="attach-document-type">${Object.entries(DOCUMENT_TYPE_LABELS).map(([code, label]) => `<option value="${escapeHTML(code)}">${escapeHTML(label)}</option>`).join("")}</select>
      ${CyberCalcUI.uploadField({ id: "attach-file", label: "Необязательные вложения", multiple: true, prompt: "Перетащите документы сюда или выберите с диска" })}
      <button type="button" class="btn secondary" id="attach-upload">Загрузить</button>
    </div>`}
  </div>`;
}

async function wireAttachSection(root, entryId, readOnly = false, uploadControl = null) {
  const list = root.querySelector("#attach-list");
  const refresh = async () => {
    try {
      const items = (await api(`/entries/${entryId}/attachments`)) || [];
      list.innerHTML = items.length
        ? items
            .map(
              (a) =>
                `<div>📄 <a href="/api/attachments/${encodeURIComponent(a.id)}/download">${escapeHTML(a.file_name)}</a> · ${escapeHTML(DOCUMENT_TYPE_LABELS[a.document_type] || a.document_type)} <span class="status-badge ${a.review_status === "approved" ? "active" : a.review_status === "rejected" ? "inactive" : "pending"}">${a.review_status === "approved" ? "Проверен" : a.review_status === "rejected" ? "Отклонён" : "Ожидает проверки"}</span>${a.content_sha256 ? ` <code title="SHA-256: ${escapeHTML(a.content_sha256)}">SHA ${escapeHTML(a.content_sha256.slice(0, 10))}…</code>` : ""} <span class="muted">(до ${new Date(
                  a.retention_expires_at,
                ).toLocaleDateString("ru-RU")})</span>${a.review_comment ? `<small>${escapeHTML(a.review_comment)}</small>` : ""}${["super_admin", "holding_admin", "org_admin", "legal_specialist"].includes(state.me.role) && a.review_status === "pending" ? `<button type="button" class="btn secondary" data-review="${escapeHTML(a.id)}" data-status="approved">Принять</button><button type="button" class="btn secondary" data-review="${escapeHTML(a.id)}" data-status="rejected">Отклонить</button>` : ""}</div>`,
            )
            .join("")
        : `<span class="muted">Файлов пока нет</span>`;
      list.querySelectorAll("[data-review]").forEach((button) => { button.onclick = async () => {
        const rejected = button.dataset.status === "rejected";
        const comment = prompt(rejected ? "Причина отклонения (обязательно)" : "Комментарий проверяющего", "") ?? "";
        if (rejected && !comment.trim()) return;
        button.disabled = true;
        try {
          await api(`/attachments/${encodeURIComponent(button.dataset.review)}/review`, { method: "PATCH", body: JSON.stringify({ status: button.dataset.status, comment: comment.trim() }) });
          await refresh();
        } catch (error) { showToast(error.message); button.disabled = false; }
      }; });
    } catch (e) {
      list.innerHTML = `<span class="error">${escapeHTML(e.message)}</span>`;
    }
  };
  const upload = root.querySelector("#attach-upload");
  if (upload && !readOnly && !uploadControl) {
    uploadControl = CyberCalcUI.bindUpload(root.querySelector("[data-upload]"), (files) => {
      const prompt = root.querySelector(".ui-dropzone span");
      if (prompt && files.length) prompt.textContent = `Выбрано файлов: ${files.length}`;
    });
  }
  if (upload && !readOnly) upload.onclick = async () => {
    const uploadButton = root.querySelector("#attach-upload");
    if (!uploadControl.files.length) {
      showToast("Сначала выберите файл");
      return;
    }
    uploadButton.disabled = true;
    uploadButton.textContent = "Загружаем…";
    try {
      await uploadFileBatch(entryId, uploadControl.files, root.querySelector("#attach-document-type").value);
      uploadControl.clear();
      const prompt = root.querySelector(".ui-dropzone span");
      if (prompt) prompt.textContent = "Перетащите документы сюда или выберите с диска";
      await refresh();
      showToast("Файлы загружены", "success");
    } catch (e) {
      showToast(e.message);
    } finally {
      uploadButton.disabled = false;
      uploadButton.textContent = "Загрузить";
    }
  };
  refresh();
}

// ТОП-ИТ/ТОП-ИИ: составляющие программы (TOP-04, TOP-06, TOP-07) — неденежная
// поддержка, стипендиаты и производственные кейсы. Пороги и шкала прогресса не
// показываются: они ждут решения ADR-14.
const TOP_KIND_LABELS = { support: "Неденежная поддержка", scholarship: "Стипендиаты", case: "Производственные кейсы" };
const TOP_SUPPORT_KIND_LABELS = { equipment: "Оборудование", software: "Программное обеспечение" };
const TOP_CASE_STATUS_LABELS = { proposed: "Предложен", implemented: "Внедрён" };
const TOP_FIELDS = {
  support: [
    { key: "support_kind", label: "Вид поддержки", type: "select", options: TOP_SUPPORT_KIND_LABELS },
    { key: "act_reference", label: "Реквизиты акта приёма-передачи", type: "text" },
    { key: "act_date", label: "Дата акта", type: "date" },
    { key: "balance_value_rub", label: "Балансовая стоимость, ₽", type: "money" },
    { key: "appraised_value_rub", label: "Оценочная стоимость, ₽", type: "money" },
    { key: "confirmed_value_rub", label: "Подтверждённая стоимость, ₽", type: "money" },
  ],
  scholarship: [
    { key: "student_name", label: "Студент", type: "text" },
    { key: "group_name", label: "Группа", type: "text" },
    { key: "course", label: "Курс (1–6)", type: "number" },
    { key: "period_start", label: "Начало периода", type: "date" },
    { key: "period_end", label: "Конец периода", type: "date" },
    { key: "amount_rub", label: "Сумма стипендии, ₽", type: "money" },
    { key: "criterion", label: "Критерий отбора", type: "text" },
    { key: "donor_name", label: "Компания-донор", type: "text" },
  ],
  case: [
    { key: "implementation_org", label: "Организация внедрения", type: "text" },
    { key: "implementation_status", label: "Статус внедрения", type: "select", options: TOP_CASE_STATUS_LABELS },
    { key: "implemented_on", label: "Дата внедрения (только у внедрённого)", type: "date" },
    { key: "description", label: "Описание", type: "text" },
  ],
};

function topItemSummary(item) {
  const money = (value) => (value == null ? "—" : `${Number(value).toLocaleString("ru-RU", { minimumFractionDigits: 2 })} ₽`);
  if (item.kind === "support")
    return `${TOP_SUPPORT_KIND_LABELS[item.support_kind] || item.support_kind} · акт ${item.act_reference || "—"} от ${String(item.act_date || "").split("-").reverse().join(".")} · подтверждено ${money(item.confirmed_value_rub)}`;
  if (item.kind === "scholarship")
    return `${item.student_name}, ${item.group_name}, ${item.course} курс · ${item.period_start} — ${item.period_end} · ${money(item.amount_rub)} · донор ${item.donor_name}`;
  return `${item.implementation_org} · ${TOP_CASE_STATUS_LABELS[item.implementation_status] || item.implementation_status}${item.implemented_on ? " " + item.implemented_on : ""}`;
}

function renderTopItemsSection() {
  return `<div class="card" id="top-items-section" style="margin-top:14px;background:transparent;padding:0;border:none">
    <h2>Составляющие программы</h2>
    <p class="muted">Неденежная поддержка, стипендиаты и производственные кейсы. Суммы здесь — сведения о программе; зачётный объём определяет отчёт о софинансировании.</p>
    <div id="top-items-list" class="attach-list muted">Загрузка…</div>
    <div id="top-items-form-box"></div>
  </div>`;
}

async function wireTopItems(root, entryId, canEdit) {
  const list = root.querySelector("#top-items-list");
  const formBox = root.querySelector("#top-items-form-box");
  const refresh = async () => {
    try {
      const data = await api(`/entries/${encodeURIComponent(entryId)}/top-items`);
      const items = data?.items || [];
      const summary = data?.summary || {};
      list.className = "attach-list";
      list.innerHTML = ["support", "scholarship", "case"]
        .map((kind) => {
          const rows = items.filter((item) => item.kind === kind);
          return `<h3>${escapeHTML(TOP_KIND_LABELS[kind])} · ${rows.length}</h3>${rows.length
            ? rows.map((item) => `<div class="top-item-row"><span><b>${escapeHTML(item.title)}</b> — ${escapeHTML(topItemSummary(item))}</span>${canEdit ? `<button type="button" class="btn secondary" data-top-delete="${escapeHTML(item.id)}">Удалить</button>` : ""}</div>`).join("")
            : '<p class="muted">Строк пока нет.</p>'}`;
        })
        .join("");
      if (summary.supports || summary.scholarships || summary.cases)
        list.insertAdjacentHTML("beforeend", `<p class="muted">Подтверждённая поддержка: ${Number(summary.confirmed_support_rub || 0).toLocaleString("ru-RU", { minimumFractionDigits: 2 })} ₽ (без подтверждения: ${Number(summary.unconfirmed_supports || 0)}); стипендии: ${Number(summary.scholarships_total_rub || 0).toLocaleString("ru-RU", { minimumFractionDigits: 2 })} ₽; внедрено кейсов: ${Number(summary.implemented_cases || 0)} из ${Number(summary.cases || 0)}.</p>`);
      list.querySelectorAll("[data-top-delete]").forEach((button) => {
        button.onclick = async () => {
          if (!confirm("Удалить строку?")) return;
          button.disabled = true;
          try {
            await api(`/top-items/${encodeURIComponent(button.dataset.topDelete)}`, { method: "DELETE" });
            await refresh();
          } catch (error) {
            showToast(error.message);
            button.disabled = false;
          }
        };
      });
    } catch (error) {
      list.className = "error";
      list.textContent = error.message;
    }
  };
  if (canEdit) {
    formBox.innerHTML = `<form id="top-item-form" class="top-item-form"><div class="field"><label>Что добавить</label><select name="kind">${Object.entries(TOP_KIND_LABELS).map(([code, label]) => `<option value="${code}">${escapeHTML(label)}</option>`).join("")}</select></div>
      <div class="field"><label>Название *</label><input name="title" maxlength="300" required></div><div id="top-item-fields" class="grid cols-2"></div>
      <button type="submit" class="btn secondary">Добавить строку</button></form>`;
    const form = formBox.querySelector("form");
    const fields = form.querySelector("#top-item-fields");
    const drawFields = () => {
      fields.innerHTML = TOP_FIELDS[form.elements.kind.value]
        .map((field) => `<div class="field"><label>${escapeHTML(field.label)}</label>${field.type === "select"
          ? `<select name="${field.key}">${Object.entries(field.options).map(([code, label]) => `<option value="${code}">${escapeHTML(label)}</option>`).join("")}</select>`
          : `<input name="${field.key}" type="${field.type === "money" ? "text" : field.type}" ${field.type === "money" ? 'inputmode="decimal"' : ""}>`}</div>`)
        .join("");
    };
    form.elements.kind.onchange = drawFields;
    drawFields();
    form.onsubmit = async (event) => {
      event.preventDefault();
      const kind = form.elements.kind.value;
      const body = { kind, title: form.elements.title.value.trim() };
      for (const field of TOP_FIELDS[kind]) {
        const raw = String(form.elements[field.key].value || "").trim().replace(",", ".");
        if (raw === "") continue;
        body[field.key] = field.type === "number" ? Number(raw) : raw;
      }
      const submit = form.querySelector("button[type=submit]");
      submit.disabled = true;
      try {
        await api(`/entries/${encodeURIComponent(entryId)}/top-items`, { method: "POST", body: JSON.stringify(body) });
        form.reset();
        drawFields();
        await refresh();
        showToast("Строка добавлена", "success");
      } catch (error) {
        showToast(error.message);
      } finally {
        submit.disabled = false;
      }
    };
  }
  refresh();
}

// ----------------------------------------------------------------- ADMIN --

async function renderAdmin(root) {
  const isAdmin = state.me.role === "super_admin";
  const showEducationDirectory = canReviewEducationDirectory();
  const showITDirectory = canViewITCompanies();
  const initialDirectoryTab = showEducationDirectory ? "partners" : "it-companies";
  root.innerHTML = `<section class="page-heading"><div><h1>Управление</h1></div></section>
  <div class="admin-layout">
    <nav class="admin-nav" aria-label="Разделы административной панели">
      ${showEducationDirectory ? `<button data-t="partners"${initialDirectoryTab === "partners" ? ' class="active"' : ""}><b>Справочник ОО <span class="nav-count" data-directory-proposal-count aria-live="polite" hidden></span></b></button>` : ""}
      ${showITDirectory ? `<button data-t="it-companies"${initialDirectoryTab === "it-companies" ? ' class="active"' : ""}><b>ИТ-компании</b></button>` : ""}
      ${isAdmin ? '<button data-t="users"><b>Пользователи</b></button><button data-t="okz"><b>Справочник ОКЗ</b></button><button data-t="settings"><b>Настройки</b></button><button data-t="logs"><b>Журнал изменений</b></button>' : ""}
    </nav>
    <div id="admin-content"></div>
  </div>`;
  const box = root.querySelector("#admin-content");
  root.querySelectorAll(".admin-nav button").forEach((b) => {
    b.onclick = () => {
      root
        .querySelectorAll(".admin-nav button")
        .forEach((x) => x.classList.remove("active"));
      b.classList.add("active");
      renderAdminTab(box, b.dataset.t);
    };
  });
  renderAdminTab(box, initialDirectoryTab);
}

async function renderAdminTab(box, tab) {
  box.innerHTML = `<div class="card loading-state"><span class="spinner"></span>Загрузка данных…</div>`;
  try {
    const adminOnly = new Set(["users", "okz", "settings", "logs"]);
    if (adminOnly.has(tab) && state.me.role !== "super_admin")
      throw new Error("Этот раздел доступен только администратору");
    if (tab === "users") return await renderAdminUsers(box);
    if (tab === "okz") return await CyberCalcOKZ.render(box, { api, escapeHTML, showToast });
    if (tab === "partners") return await renderPartnerDirectory(box, true);
    if (tab === "it-companies") {
      if (!canViewITCompanies()) throw new Error("Реестр ИТ-компаний недоступен для этого профиля");
      return await renderITCompanies(box, true);
    }
    if (tab === "settings") return await renderAdminSettings(box);
    if (tab === "logs") return await renderAdminLogs(box);
    throw new Error("Неизвестный раздел администрирования");
  } catch (e) {
    box.innerHTML = `<div class="card error-state"><b>Не удалось загрузить раздел</b><span>${escapeHTML(e.message)}</span><button type="button" class="btn secondary" id="admin-retry">Повторить</button></div>`;
    box.querySelector("#admin-retry").onclick = () => renderAdminTab(box, tab);
  }
}

async function renderAdminUsers(box) {
  state.itCompanies = (await api("/admin/it-company-options")) || [];
  box.innerHTML = `<div class="section-intro"><h2>Пользователи</h2></div><div class="card"><h2>Новый пользователь</h2>
    <form id="u-form" novalidate>
    <div class="grid cols-3">
      <div class="field"><label>Электронная почта *</label><input id="u-email" type="email" maxlength="254" autocomplete="off" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Пароль *</label><input id="u-password" type="password" minlength="10" maxlength="128" autocomplete="new-password" required><div class="field-hint">10–128 символов: A–Z, a–z, цифра и спецсимвол</div><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>ФИО *</label><input id="u-name" minlength="2" maxlength="200" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Роль</label><select id="u-role"><option value="super_admin">Главный администратор</option><option value="holding_admin">Администратор холдинга</option><option value="org_admin">Администратор организации</option><option value="curator" selected>Куратор</option><option value="hr_specialist">Кадровая служба (HR)</option><option value="financial_specialist">Финансовая служба</option><option value="legal_specialist">Юридическое управление</option><option value="auditor_viewer">Аудитор (только чтение)</option></select><div class="field-hint">Права назначаются по матрице ролей; аудитор работает только в режиме чтения.</div></div>
      <div class="field"><label>Тип пользователя</label><select id="u-entity"><option value="organization">ИТ-компания</option><option value="edu_institution">Учебное заведение</option></select></div>
      <div class="field" id="u-partner-field" hidden><label id="u-partner-label">Учебное заведение *</label><select id="u-partner"><option value="">Выберите учебное заведение</option>${(Array.isArray(
        state.partners,
      )
        ? state.partners
        : []
      )
        .map(
          (partner) =>
            `<option value="${escapeHTML(partner.id)}">${escapeHTML(partner.name)}</option>`,
        )
        .join("")}</select></div>
      <div class="field" id="u-company-field"><label id="u-company-label">ИТ-компания *</label><select id="u-company"><option value="">Выберите ИТ-компанию</option>${state.itCompanies.map((company) => `<option value="${escapeHTML(company.id)}">${escapeHTML(company.name)} · ИНН ${escapeHTML(company.inn)}</option>`).join("")}</select></div>
    </div>
    <button type="submit" class="btn" id="u-create">Создать</button>
    <div class="error" id="u-error" style="display:none"></div>
    </form>
  </div>
  <div class="card"><h2>Список пользователей</h2>
    <form id="u-search-form" class="directory-search user-search" role="search">
      <div class="field"><label for="u-search">Поиск пользователя</label><input id="u-search" type="search" maxlength="200" autocomplete="off" placeholder="Введите ФИО или email"></div>
      <div class="flex"><button class="btn secondary" type="submit" id="u-find">Найти</button><button class="btn secondary" type="button" id="u-search-clear" hidden>Сбросить</button></div>
    </form>
    <p class="muted" id="u-search-status" aria-live="polite"></p>
    <div id="u-list" class="loading-state"><span class="spinner"></span>Загрузка пользователей…</div>
  </div>`;

  const entitySelect = box.querySelector("#u-entity");
  const roleSelect = box.querySelector("#u-role");
  const syncUserType = () => {
    const education = entitySelect.value === "edu_institution";
    const curator = !education && roleSelect.value === "curator";
    box.querySelector("#u-partner-field").hidden = !education && !curator;
    box.querySelector("#u-company-field").hidden = education;
    const companyRequired = roleSelect.value !== "super_admin";
    box.querySelector("#u-partner-label").textContent = education ? "Учебное заведение *" : "Закреплённая ОО куратора";
    box.querySelector("#u-company-label").textContent = `ИТ-компания${companyRequired ? " *" : ""}`;
    if (!education && !curator)
      box.querySelector("#u-partner").value = "";
    if (education) box.querySelector("#u-company").value = "";
  };
  entitySelect.onchange = syncUserType;
  roleSelect.onchange = syncUserType;
  syncUserType();

  box.querySelector("#u-form").onsubmit = async (event) => {
    event.preventDefault();
    const err = box.querySelector("#u-error");
    const form = event.currentTarget;
    clearFieldErrors(form);
    err.style.display = "none";
    const emailInput = box.querySelector("#u-email");
    const passwordInput = box.querySelector("#u-password");
    const nameInput = box.querySelector("#u-name");
    const email = emailInput.value.trim().toLowerCase();
    const password = passwordInput.value;
    const fullName = nameInput.value.trim().replace(/\s+/g, " ");
    let firstInvalid = null;
    const mark = (input, message) => {
      setFieldError(input, message);
      firstInvalid ||= input;
    };
    if (!email || !emailInput.validity.valid || email.length > 254)
      mark(emailInput, "Введите корректный email");
    if (
      fullName.length < 2 ||
      fullName.length > 200 ||
      !/\p{L}/u.test(fullName)
    )
      mark(nameInput, "Введите ФИО от 2 до 200 символов");
    if (password.length < 10 || password.length > 128)
      mark(passwordInput, "Пароль должен содержать от 10 до 128 символов");
    else if (
      /\s/.test(password) ||
      !/[a-zа-яё]/u.test(password) ||
      !/[A-ZА-ЯЁ]/u.test(password) ||
      !/\d/u.test(password) ||
      !/[^\p{L}\p{N}\s]/u.test(password)
    ) {
      mark(
        passwordInput,
        "Добавьте строчную и заглавную буквы, цифру и специальный символ",
      );
    }
    if (
      entitySelect.value === "edu_institution" ||
      roleSelect.value !== "super_admin"
    ) {
      const assignmentInput = entitySelect.value === "edu_institution"
        ? box.querySelector("#u-partner")
        : box.querySelector("#u-company");
      if (!assignmentInput.value)
        mark(
          assignmentInput,
          entitySelect.value === "edu_institution"
            ? "Выберите учебное заведение"
            : "Выберите ИТ-компанию",
        );
    }
    if (firstInvalid) {
      firstInvalid.focus();
      return;
    }

    const createButton = box.querySelector("#u-create");
    createButton.disabled = true;
    createButton.textContent = "Создаём…";
    try {
      const partnerValue = box.querySelector("#u-partner").value;
      const companyValue = box.querySelector("#u-company").value;
      await api("/admin/users", {
        method: "POST",
        body: JSON.stringify({
          email,
          password,
          full_name: fullName,
          role: box.querySelector("#u-role").value,
          entity_type: entitySelect.value,
          partner_id: partnerValue || null,
          it_company_id: companyValue || null,
        }),
      });
      showToast("Пользователь создан", "success");
      await renderAdminTab(box, "users");
    } catch (e) {
      err.textContent = e.message;
      err.style.display = "block";
    } finally {
      createButton.disabled = false;
      createButton.textContent = "Создать";
    }
  };

  const searchInput = box.querySelector("#u-search");
  const searchStatus = box.querySelector("#u-search-status");
  const clearSearch = box.querySelector("#u-search-clear");
  const listBox = box.querySelector("#u-list");
  let requestGeneration = 0;
  const loadUsers = async () => {
    const generation = ++requestGeneration;
    const query = searchInput.value.trim().replace(/\s+/g, " ");
    searchInput.value = query;
    clearSearch.hidden = !query;
    searchStatus.textContent = "";
    listBox.className = "loading-state";
    listBox.innerHTML = '<span class="spinner"></span>Поиск пользователей…';
    try {
      const users =
        (await api(`/admin/users?q=${encodeURIComponent(query)}`)) || [];
      if (generation !== requestGeneration) return;
      listBox.className = "";
      searchStatus.textContent = query
        ? `Найдено пользователей: ${users.length}`
        : `Всего пользователей: ${users.length}`;
      listBox.innerHTML = users.length
        ? `<div class="table-wrap"><table><thead><tr><th>Электронная почта</th><th>ФИО</th><th>Организация</th><th>Закреплённая ОО</th><th>Роль</th><th>Статус</th><th></th></tr></thead>
          <tbody>${users
            .map(
              (u) => `<tr>
            <td>${escapeHTML(u.email)}</td><td>${escapeHTML(u.full_name)}</td><td>${escapeHTML(u.entity_type === "edu_institution" ? "Образовательная организация" : state.itCompanies.find((company) => company.id === u.it_company_id)?.name || "ИТ-компания не назначена")}</td><td>${escapeHTML(state.partners.find((partner) => partner.id === u.partner_id)?.name || "—")}</td><td><span class="role-badge">${escapeHTML(valueLabel(u.role))}</span></td>
            <td><span class="status-badge ${u.is_active ? "active" : "inactive"}">${u.is_active ? "Активен" : "Отключён"}</span></td>
            <td><button class="btn secondary" data-id="${escapeHTML(u.id)}" data-active="${u.is_active}">${u.is_active ? "Отключить" : "Включить"}</button><button class="btn secondary" data-profile="${escapeHTML(u.id)}">Профиль и доступ</button></td>
          </tr>`,
            )
            .join("")}</tbody></table></div>`
        : `<div class="empty-state"><b>Пользователи не найдены</b><span>Измените запрос.</span></div>`;
      listBox.querySelectorAll("button[data-id]").forEach((button) => {
        button.onclick = async () => {
          button.disabled = true;
          try {
            await api(`/admin/users/${button.dataset.id}`, {
              method: "PATCH",
              body: JSON.stringify({
                is_active: button.dataset.active !== "true",
              }),
            });
            showToast("Статус пользователя обновлён", "success");
            await loadUsers();
          } catch (error) {
            showToast(error.message);
            button.disabled = false;
          }
        };
      });
      listBox.querySelectorAll("[data-profile]").forEach((button) => {
        button.onclick = () =>
          openUserProfile(
            users.find((user) => user.id === button.dataset.profile),
            loadUsers,
          );
      });
    } catch (error) {
      if (generation !== requestGeneration) return;
      listBox.className = "error-state";
      listBox.innerHTML = `<b>Не удалось найти пользователей</b><span>${escapeHTML(error.message)}</span>`;
    }
  };
  box.querySelector("#u-search-form").onsubmit = (event) => {
    event.preventDefault();
    loadUsers();
  };
  clearSearch.onclick = () => {
    searchInput.value = "";
    searchInput.focus();
    loadUsers();
  };
  await loadUsers();
}

async function renderAdminSettings(box) {
  const settings = await api("/admin/settings");
  const companies = (await api("/admin/it-company-options")) || [];
  box.innerHTML = `<div class="card"><h2>Настройки хранения</h2>
    <div class="grid cols-2">
      <div class="field"><label>Хранение подтверждающих документов, дней</label>
        <input id="s-attach" type="number" min="1" max="3650" step="1" value="${settings.attachment_retention_days || 365}"></div>
      <div class="field"><label>Хранение журнала изменений, дней</label>
        <input id="s-audit" type="number" min="60" max="3650" step="1" value="${settings.audit_log_retention_days || 60}"></div>
      ${state.me.role === "super_admin" ? `<div class="field"><label>Выход при бездействии, минут</label>
        <input id="s-session-idle" type="number" min="5" max="1440" step="1" value="${settings.session_idle_timeout_minutes || 30}"></div>` : ""}
    </div>
    <button class="btn" id="s-save">Сохранить</button>
    <p class="field-hint">Журнал изменений хранится не менее 60 дней.</p>
  </div>
  <div class="card"><div class="flex between"><div><h2>Неизменяемые снимки на 1 мая</h2></div><span class="status-badge">Москва (UTC+3)</span></div>
    <div class="grid cols-3">
      <div class="field"><label>ИТ-компания</label><select id="snapshot-company"><option value="">Выберите компанию</option>${companies.map((company) => `<option value="${escapeHTML(company.id)}">${escapeHTML(company.name)} · ИНН ${escapeHTML(company.inn)}</option>`).join("")}</select></div>
      <div class="field"><label>Отчётный год</label><input id="snapshot-year" type="number" min="2000" max="2100" value="${state.year}"></div>
      <div class="field"><label>&nbsp;</label><button class="btn" id="snapshot-create" type="button">Сформировать снимок</button></div>
    </div>
    <p class="field-hint">Формирование разрешено только 1 мая выбранного отчётного года по московскому времени. Повторная запись за тот же год запрещена.</p>
    <div id="snapshot-list" class="loading-state"><span class="spinner"></span>Загрузка снимков…</div>
  </div>`;
  box.querySelector("#s-save").onclick = async () => {
    const button = box.querySelector("#s-save");
    const attachmentDays = Number(box.querySelector("#s-attach").value);
    const auditDays = Number(box.querySelector("#s-audit").value);
    const idleInput = box.querySelector("#s-session-idle");
    const idleMinutes = idleInput ? Number(idleInput.value) : null;
    if (auditDays < 60) { showToast("Журнал аудита хранится минимум 60 дней"); return; }
    if (
      ![attachmentDays, auditDays].every(
        (value) => Number.isInteger(value) && value >= 1 && value <= 3650,
      )
    ) {
      showToast("Срок хранения должен быть целым числом от 1 до 3650 дней");
      return;
    }
    if (idleInput && (!Number.isInteger(idleMinutes) || idleMinutes < 5 || idleMinutes > 1440)) {
      showToast("Выход при бездействии задаётся целым числом от 5 до 1440 минут");
      return;
    }
    button.disabled = true;
    button.textContent = "Сохраняем…";
    try {
      await api("/admin/settings", {
        method: "POST",
        body: JSON.stringify({
          key: "attachment_retention_days",
          value: String(attachmentDays),
        }),
      });
      if (idleInput) await api("/admin/settings", {
        method: "POST",
        body: JSON.stringify({ key: "session_idle_timeout_minutes", value: String(idleMinutes) }),
      });
      await api("/admin/settings", {
        method: "POST",
        body: JSON.stringify({
          key: "audit_log_retention_days",
          value: String(auditDays),
        }),
      });
      showToast("Настройки сохранены", "success");
      await renderAdminTab(box, "settings");
    } catch (e) {
      showToast(e.message);
      button.disabled = false;
      button.textContent = "Сохранить";
    }
  };

  const companySelect = box.querySelector("#snapshot-company");
  const snapshotList = box.querySelector("#snapshot-list");
  const loadSnapshots = async () => {
    if (!companySelect.value) {
      snapshotList.className = "empty-state";
      snapshotList.innerHTML = "<span>Выберите ИТ-компанию, чтобы увидеть снимки.</span>";
      return;
    }
    snapshotList.className = "loading-state";
    snapshotList.innerHTML = '<span class="spinner"></span>Загрузка снимков…';
    try {
      const items = await api(`/report-snapshots?it_company_id=${encodeURIComponent(companySelect.value)}`);
      snapshotList.className = "";
      snapshotList.innerHTML = items.length
        ? `<div class="table-wrap"><table><thead><tr><th>Дата среза</th><th>Зафиксирован</th><th>Состав</th><th>SHA-256</th><th></th></tr></thead><tbody>${items.map((item) => `<tr><td>${escapeHTML(item.snapshot_date)}</td><td>${new Date(item.captured_at).toLocaleString("ru-RU")}</td><td>${item.entries_count} мероприятий · ${item.documents_count} документов</td><td><code title="${escapeHTML(item.sha256)}">${escapeHTML(item.sha256.slice(0, 16))}…</code></td><td><a class="btn secondary" href="/api/report-snapshots/${encodeURIComponent(item.id)}?it_company_id=${encodeURIComponent(companySelect.value)}">Скачать JSON</a></td></tr>`).join("")}</tbody></table></div>`
        : '<div class="empty-state"><b>Снимков пока нет</b><span>Система разрешит фиксацию 1 мая отчётного года.</span></div>';
    } catch (error) {
      snapshotList.className = "error-state";
      snapshotList.textContent = error.message;
    }
  };
  companySelect.onchange = loadSnapshots;
  box.querySelector("#snapshot-create").onclick = async (event) => {
    const year = Number(box.querySelector("#snapshot-year").value);
    if (!companySelect.value || !Number.isInteger(year) || year < 2000 || year > 2100) {
      showToast("Выберите ИТ-компанию и корректный отчётный год");
      return;
    }
    const button = event.currentTarget;
    button.disabled = true;
    try {
      await api(`/report-snapshots?it_company_id=${encodeURIComponent(companySelect.value)}&report_year=${year}`, { method: "POST" });
      showToast("Неизменяемый снимок сформирован", "success");
      await loadSnapshots();
    } catch (error) {
      showToast(error.message);
    } finally {
      button.disabled = false;
    }
  };
  await loadSnapshots();
}

async function renderAdminLogs(box) {
  box.innerHTML = `<div class="section-intro"><h2>Журнал изменений</h2></div><div class="card"><div class="flex between"><h2>Последние действия</h2><div class="tabs"><button class="active" data-log-filter="all">Все</button><button data-log-filter="directory_confirm">Подтверждения справочника</button></div></div><div id="logs-list">Загрузка…</div></div>`;
  const logs = (await api("/admin/logs?limit=200")) || [];
  const fieldLabels = {
    name: "Наименование", partner_kind: "Тип", region: "Регион", inn: "ИНН", ogrn: "ОГРН",
    license_number: "Лицензия", license_status: "Статус лицензии",
    institution_status: "Статус организации", registry_record_id: "Запись реестра",
    source_url: "Источник", registry_updated_at: "Дата актуальности",
    verification_status: "Статус проверки",
    version: "Версия", records: "Количество записей", effective_on: "Дата начала действия",
  };
  const summarize = (log) => {
	const before = log.old || log.old_value || {};
	const after = log.new || log.new_value || {};
    const changed = [...new Set([...Object.keys(before), ...Object.keys(after)])]
      .filter((key) => JSON.stringify(before[key] ?? "") !== JSON.stringify(after[key] ?? ""))
      .map((key) => `${fieldLabels[key] || key}: «${before[key] || "—"}» → «${after[key] || "—"}»`);
    const name = after.name || before.name || "";
    const details = changed.length
      ? `<details><summary>${changed.length} изменённых полей</summary><ul>${changed.map((line) => `<li>${escapeHTML(line)}</li>`).join("")}</ul></details>`
      : "";
    return `${name ? `<b>${escapeHTML(name)}</b><br>` : ""}${escapeHTML(log.comment_text || "")}${details}`;
  };
  const paint = (filter = "all") => {
    const visible = filter === "all" ? logs : logs.filter((log) => log.action === filter);
    box.querySelector("#logs-list").innerHTML = visible.length
      ? `<div class="table-wrap"><table><thead><tr><th>Когда</th><th>Кто</th><th>Объект и действие</th><th>Что изменено</th></tr></thead><tbody>${visible.map((log) => `<tr>
        <td>${new Date(log.created_at).toLocaleString("ru-RU")}</td>
		<td>${escapeHTML(log.actor?.name || log.user_name || ((log.actor?.id || log.user_id) ? "Пользователь" : "Автоматический скрипт"))}${(log.actor?.email || log.user_email) ? `<br><small>${escapeHTML(log.actor?.email || log.user_email)}</small>` : ""}</td>
		<td title="ID запроса: ${escapeHTML(log.request_id || "—")}">${escapeHTML(valueLabel(log.entity?.type || log.entity_type))}${(log.entity?.id || log.entity_id) ? " #" + escapeHTML((log.entity?.id || log.entity_id).slice(0, 8)) : ""}<br><b>${escapeHTML(actionLabel(log.action))}</b></td>
        <td>${summarize(log)}</td>
      </tr>`).join("")}</tbody></table></div>`
      : '<p class="muted">Действий по выбранному фильтру пока нет.</p>';
  };
  box.querySelectorAll("[data-log-filter]").forEach((button) => {
    button.onclick = () => {
      box.querySelectorAll("[data-log-filter]").forEach((item) => item.classList.remove("active"));
      button.classList.add("active");
      paint(button.dataset.logFilter);
    };
  });
  paint();
}

CyberCalcRouter.configure(render);
CyberCalcScreenLoader.configure(() => Object.freeze({
  state,
  renderDashboard,
  renderEntries,
  renderPartners: renderPartnersScreen,
  renderReports: renderReportsScreen,
  renderSettings: renderSettingsScreen,
}));

boot();
