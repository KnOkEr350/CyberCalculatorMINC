// Калькулятор затрат по Приказу Минцифры — фронтенд.
// Без фреймворков и сборки: чистый fetch + DOM. Один файл, простой роутер по хэшу.

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
};

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
  "#1a79ff",
  "#00a6a6",
  "#6757d9",
  "#f2a51a",
  "#e85c8b",
  "#36a269",
  "#489dff",
  "#805ad5",
  "#de6f3c",
];

const VALUE_LABELS = {
  vuz: "Вуз",
  kolledj: "Колледж",
  school: "Школа",
  rpd: "РПД",
  oop: "ООП",
  vo: "Высшее образование (ВО)",
  spo: "Среднее профессиональное образование (СПО)",
  development: "Разработка",
  update: "Актуализация",
  expertise: "Экспертиза",
  user: "Пользователь",
  moderator: "Модератор",
  admin: "Администратор",
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
};

function valueLabel(value) {
  return VALUE_LABELS[value] || value;
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
  return (
    Number(v || 0).toLocaleString("ru-RU", { maximumFractionDigits: 2 }) + " ₽"
  );
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
    <span class="brand-emblem" aria-hidden="true"><svg viewBox="0 0 36 40" focusable="false"><rect class="calculator-body" x="3" y="1" width="30" height="38" rx="6"/><rect class="calculator-screen" x="8" y="6" width="20" height="8" rx="2"/><circle cx="10" cy="21" r="2"/><circle cx="18" cy="21" r="2"/><circle cx="26" cy="21" r="2"/><circle cx="10" cy="30" r="2"/><circle cx="18" cy="30" r="2"/><rect x="24" y="28" width="4" height="4" rx="1.2"/></svg></span>
    <span class="brand-copy"><strong>КИБЕРПРОТЕКТ</strong><small>Калькулятор затрат</small></span>
  </div>`;
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
  const toast = el(
    `<div class="toast ${escapeHTML(kind)}" role="status"></div>`,
  );
  toast.textContent = message;
  document.body.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add("visible"));
  setTimeout(() => {
    toast.classList.remove("visible");
    setTimeout(() => toast.remove(), 200);
  }, 3500);
}

// ---------------------------------------------------------------- ROUTER --

async function boot() {
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
    state.me.role !== "admin" &&
    (!state.me.entity_type ||
      (state.me.entity_type === "edu_institution" && !state.me.partner_id) ||
      (state.me.role === "user" &&
        state.me.entity_type === "organization" &&
        !state.me.it_company_id))
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
      <div class="auth-orbit" aria-hidden="true"><i></i><i></i><i></i></div>
    </aside>
    <main class="auth-form-panel">
      <form class="login-box" id="login-form">
        <span class="eyebrow">Личный кабинет</span>
        <h1>Вход</h1>
        <div class="field"><label for="login-email">Email</label><input type="email" id="login-email" autocomplete="username" required placeholder="name@company.ru"></div>
        <div class="field"><label for="login-password">Пароль</label><input type="password" id="login-password" autocomplete="current-password" required placeholder="Пароль"></div>
        <div class="field"><label for="login-code">Код подтверждения <span class="label-optional">необязательно</span></label><input id="login-code" autocomplete="one-time-code" maxlength="20" placeholder="6 цифр или резервный код"><div class="field-hint">Заполните, если включена двухфакторная защита.</div></div>
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

// ---------------------------------------------------------------- LAYOUT --

function renderLayout() {
  const isAdmin = state.me.role === "admin";
  const isModerator = state.me.role === "moderator";
  const isManager = isAdmin || isModerator;
  const profileLabel =
    state.me.entity_type === "organization"
      ? "ИТ-организация"
      : "Учебное заведение";
  const allowedViews = new Set(["dashboard", "entries"]);
  if (isManager) allowedViews.add("admin");
  else {
    if (canReviewEducationDirectory()) allowedViews.add("partners");
    if (canViewITCompanies()) allowedViews.add("it-companies");
  }
  if (!allowedViews.has(state.view)) state.view = "dashboard";
  const wrap = el(`<div>
    <a class="skip-link" href="#content">К содержанию</a>
    <div class="topbar">
      ${brandMarkup()}
      <nav>
        <button data-view="dashboard">Сводка</button>
        <button data-view="entries">План / Факт</button>
        ${!isManager && canReviewEducationDirectory() ? '<button data-view="partners">Учебные заведения</button>' : ""}
        ${!isManager && canViewITCompanies() ? '<button data-view="it-companies">ИТ-компании</button>' : ""}
        ${isManager ? '<button data-view="admin">Управление</button>' : ""}
      </nav>
      <div class="who">
        <span class="avatar">${escapeHTML(initials(state.me.full_name))}</span>
        <span class="user-copy"><strong>${escapeHTML(state.me.full_name)}</strong><small>${profileLabel}${isAdmin ? " · Администратор" : isModerator ? " · Модератор" : ""}</small></span>
        <button id="change-password" title="Изменить пароль">Сменить пароль</button>
        ${state.me.mfa_available && !state.me.mfa_enabled ? '<button id="setup-mfa">Настроить 2FA</button>' : ''}
        <button id="logout" title="Выйти из системы">Выйти</button>
      </div>
    </div>
    <main class="container" id="content"></main>
  </div>`);
  wrap.querySelectorAll("nav button").forEach((b) => {
    if (b.dataset.view === state.view) b.classList.add("active");
    b.onclick = () => {
      state.view = b.dataset.view;
      render();
    };
  });
  wrap.querySelector("#logout").onclick = async () => {
    try {
      await api("/auth/logout", { method: "POST" });
      state.me = null;
      render();
    } catch (e) {
      showToast(e.message);
    }
  };
  wrap.querySelector("#change-password").onclick = openPasswordDialog;
  const setupMFA = wrap.querySelector("#setup-mfa");
  if (setupMFA) setupMFA.onclick = () => { app.replaceChildren(renderMFASetup()); };
  const content = wrap.querySelector("#content");
  if (state.view === "dashboard") renderDashboard(content);
  else if (state.view === "entries") renderEntries(content);
  else if (state.view === "partners")
    renderPartnerDirectory(content).catch((e) => showToast(e.message));
  else if (state.view === "it-companies")
    renderITCompanies(content).catch((e) => showToast(e.message));
  else if (state.view === "admin") renderAdmin(content);
  else {
    state.view = "dashboard";
    renderDashboard(content);
  }
  return wrap;
}

function openPasswordDialog() {
  const modal = el(`<div class="modal-backdrop"><div class="card modal"><h2>Смена пароля</h2>
    <p>После смены пароля потребуется войти заново на всех устройствах.</p>
    <form><label>Текущий пароль<input name="current" type="password" autocomplete="current-password" required maxlength="128"></label>
    <label>Новый пароль<input name="next" type="password" autocomplete="new-password" required minlength="10" maxlength="128"></label>
    <label>Повторите новый пароль<input name="repeat" type="password" autocomplete="new-password" required maxlength="128"></label>
    <p>10–128 символов: заглавная и строчная буквы, цифра и специальный символ.</p>
    <p role="alert" class="error"></p><button class="btn" type="submit">Сменить пароль</button><button class="btn secondary" type="button">Отмена</button></form></div></div>`);
  document.body.append(modal);
  const form = modal.querySelector("form");
  form.querySelector('[type="button"]').onclick = () => modal.remove();
  form.onsubmit = async (event) => {
    event.preventDefault();
    const error = form.querySelector('[role="alert"]');
    if (form.elements.next.value !== form.elements.repeat.value) { error.textContent = "Пароли не совпадают"; return; }
    const button = form.querySelector('[type="submit"]'); button.disabled = true;
    try {
      await api("/auth/password", { method: "POST", body: JSON.stringify({current_password: form.elements.current.value, new_password: form.elements.next.value}) });
      form.reset(); modal.remove(); state.me = null; render(); showToast("Пароль изменён. Войдите с новым паролем.");
    } catch (e) { error.textContent = e.message; } finally { button.disabled = false; }
  };
}

// ------------------------------------------------------------- DASHBOARD --

async function renderDashboard(root) {
  root.appendChild(el(`<div class="muted">Загрузка дашборда…</div>`));
  const fixedEducationPartner =
    state.me.role === "user" && state.me.entity_type === "edu_institution";
  if (fixedEducationPartner) state.partnerID = state.me.partner_id || "";
  let d;
  try {
    d = await api(
      `/dashboard?report_year=${state.year}&partner_id=${encodeURIComponent(state.partnerID)}`,
    );
  } catch (e) {
    root.innerHTML = `<div class="error">${escapeHTML(e.message)}</div>`;
    return;
  }
  state.dashboard = d;

  const breakdownList = (arr) =>
    arr && arr.length
      ? arr
          .map(
            (b) => `<div class="bar-row">
        <div class="name">${escapeHTML(CATEGORY_LABELS[b.category_code] || b.category_code)}</div>
        <div class="bar-track"><div class="bar-fill" style="width:${clampPercent(b.share_percent)}%"></div></div>
        <div class="bar-value">${escapeHTML(fmtMoney(b.amount_rub))}</div>
      </div>`,
          )
          .join("")
      : `<div class="muted">Нет данных за ${state.year} год</div>`;

  const groupedChart = (plan, fact) => {
    const planMap = new Map(
      (plan || []).map((item) => [
        item.category_code,
        Number(item.amount_rub) || 0,
      ]),
    );
    const factMap = new Map(
      (fact || []).map((item) => [
        item.category_code,
        Number(item.amount_rub) || 0,
      ]),
    );
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
          const planAmount = planMap.get(code) || 0;
          const factAmount = factMap.get(code) || 0;
          return `<div class="compare-row">
            <div class="compare-label" title="${escapeHTML(CATEGORY_LABELS[code] || code)}">${escapeHTML(CATEGORY_LABELS[code] || code)}</div>
            <div class="compare-bars">
              <div class="compare-bar plan" style="width:${(planAmount / max) * 100}%"><span>${escapeHTML(fmtMoney(planAmount))}</span></div>
              <div class="compare-bar fact" style="width:${(factAmount / max) * 100}%"><span>${escapeHTML(fmtMoney(factAmount))}</span></div>
            </div>
          </div>`;
        })
        .join("")}
    </div>`;
  };

  const donutChart = (items, total, title) => {
    if (!items || !items.length || !Number(total)) {
      return `<div class="donut-panel"><div class="chart-empty">Нет данных для диаграммы «${escapeHTML(title)}»</div></div>`;
    }
    let cursor = 0;
    const segments = items.map((item, index) => {
      const start = cursor;
      cursor += clampPercent(item.share_percent);
      return `${CHART_COLORS[index % CHART_COLORS.length]} ${start}% ${cursor}%`;
    });
    return `<div class="donut-panel">
      <div class="donut" style="background:conic-gradient(${segments.join(",")})">
        <div class="donut-hole"><span>${escapeHTML(title)}</span><strong>${escapeHTML(fmtMoney(total))}</strong></div>
      </div>
      <div class="donut-legend">${items
        .map(
          (item, index) =>
            `<div><i style="background:${CHART_COLORS[index % CHART_COLORS.length]}"></i><span>${escapeHTML(
              CATEGORY_LABELS[item.category_code] || item.category_code,
            )}</span><b>${Number(item.share_percent || 0).toLocaleString("ru-RU")}%</b></div>`,
        )
        .join("")}</div>
    </div>`;
  };

  const planPct = clampPercent(d.plan_completion_pct);
  const targetPct = d.target_amount_rub
    ? clampPercent(
        (Number(d.fact_total_rub) / Number(d.target_amount_rub)) * 100,
      )
    : 0;
  const selectedPartner = state.partners.find(
    (partner) => partner.id === state.partnerID,
  );
  const aggregateDashboard = isStaffUser() && !state.partnerID;
  const partnerFilter = fixedEducationPartner
    ? `<div class="field"><label>Учебное заведение</label><input value="${escapeHTML(selectedPartner?.name || "Назначенное учебное заведение")}" readonly></div>`
    : `<div class="field"><label for="dash-partner">Учебное заведение</label><select id="dash-partner"><option value="">Все учебные заведения</option>${state.partners.map((p) => `<option value="${p.id}" ${p.id === state.partnerID ? "selected" : ""}>${escapeHTML(p.name)}</option>`).join("")}</select></div>`;

  root.innerHTML = `
    <section class="page-heading">
      <div><h1>Сводка</h1></div>
      <span class="year-badge">${state.year}</span>
    </section>
    <div class="card">
      <div class="dashboard-toolbar">
        <h2 style="margin:0">Показатели</h2>
        <div class="dashboard-filters">
          <div class="field"><label for="dash-year">Год</label><input type="number" id="dash-year" min="2000" max="2100" step="1" value="${state.year}"></div>
          ${partnerFilter}
          ${
            aggregateDashboard
              ? `<div class="field"><label aria-hidden="true">Целевая сумма</label><button class="btn secondary" id="set-target">Задать целевую сумму (3%)</button></div>`
              : ""
          }
        </div>
      </div>
      <div class="grid cols-3" style="margin-top:14px">
        ${
          aggregateDashboard
            ? `<div class="stat"><div class="label">Общая целевая сумма (3% от льгот)</div><div class="value">${d.target_amount_rub != null ? fmtMoney(d.target_amount_rub) : "не задана"}</div></div>`
            : `<div class="stat"><div class="label">Учебное заведение</div><div class="value">${escapeHTML(selectedPartner?.name || "не выбрано")}</div></div>`
        }
        <div class="stat"><div class="label">План: все расчёты, руб.</div><div class="value">${fmtMoney(d.plan_total_rub)}</div></div>
        <div class="stat"><div class="label">Факт: все расчёты, руб.</div><div class="value">${fmtMoney(d.fact_total_rub)}</div></div>
      </div>
      <div class="progress-card" style="margin-top:10px">
        <div class="progress-header"><span>Реализация плана</span><strong>${Number(d.plan_completion_pct || 0).toLocaleString("ru-RU")}%</strong></div>
        <div class="progress-track"><div class="progress-fill" style="width:${planPct}%"></div></div>
        ${
          d.target_amount_rub
            ? `<div class="progress-header target"><span>Выполнение минимального объёма (3%)</span><strong>${
                Math.round(
                  (Number(d.fact_total_rub) / Number(d.target_amount_rub)) *
                    10000,
                ) / 100
              }%</strong></div><div class="progress-track"><div class="progress-fill target" style="width:${targetPct}%"></div></div>`
            : ""
        }
      </div>
    </div>
    <div class="card"><h2>План и факт по категориям</h2>${groupedChart(d.plan_by_category, d.fact_by_category)}</div>
    <div class="grid cols-2">
      <div class="card approved-summary"><h2>После утверждения</h2><div class="approved-values"><div><span>План</span><strong>${fmtMoney(d.eligible_plan_total_rub)}</strong></div><div><span>Факт</span><strong>${fmtMoney(d.eligible_fact_total_rub)}</strong></div></div><p class="muted">Не учтено записей: ${Number(d.incomplete_entries || 0)}</p><details class="rules-note"><summary>Как учитываются суммы</summary><p>Сумма учитывается после проверки и утверждения полного комплекта по соглашению. Для ТОП ИТ/ИИ действует предусмотренное приказом исключение.</p></details></div>
      <div class="card"><h2>Структура плана</h2>${donutChart(d.plan_by_category, d.plan_total_rub, "План")}</div>
      <div class="card"><h2>Структура факта</h2>${donutChart(d.fact_by_category, d.fact_total_rub, "Факт")}</div>
    </div>
    <div class="grid cols-2">
      <div class="card"><h2>Детализация — План</h2>${breakdownList(d.plan_by_category)}</div>
      <div class="card"><h2>Детализация — Факт</h2>${breakdownList(d.fact_by_category)}</div>
    </div>
  `;
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
  const targetBtn = root.querySelector("#set-target");
  if (targetBtn) {
    targetBtn.onclick = async () => {
      const val = prompt(
        "Целевая сумма затрат на " +
          state.year +
          " год, руб. (3% от сэкономленных льгот):",
        d.target_amount_rub || "",
      );
      if (val == null) return;
      const amount = Number(String(val).replace(",", "."));
      if (!Number.isFinite(amount) || amount <= 0) {
        alert("Введите положительную целевую сумму.");
        return;
      }
      targetBtn.disabled = true;
      try {
        await api("/dashboard/target", {
          method: "POST",
          body: JSON.stringify({
            report_year: state.year,
            target_amount_rub: amount,
          }),
        });
        showToast("Целевая сумма сохранена", "success");
        render();
      } catch (e) {
        showToast(e.message);
        targetBtn.disabled = false;
      }
    };
  }
}

// --------------------------------------------------------------- ENTRIES --

async function renderEntries(root) {
  try {
    await renderPartnerEntries(root);
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
          : (f.options || []).map((o) => ({
              v: o,
              l:
                {
                  rpd: "РПД",
                  oop: "ООП",
                  vo: "Высшее образование",
                  spo: "Среднее профессиональное",
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
    return `<input type="number" min="0" max="1000000000000" step="${f.integer ? "1" : "any"}" data-key="${escapeHTML(
      f.key,
    )}" data-kind="number" value="${escapeHTML(val)}" ${f.required ? "required" : ""}>`;
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
    if (!raw && field.required) {
      message = "Поле обязательно";
    } else if (raw) {
      if (field.type === "number") {
        const number = Number(raw);
        if (!Number.isFinite(number)) message = "Введите корректное число";
        else if (number < 0) message = "Значение не может быть отрицательным";
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

async function openEntryModal(entry) {
  const cat = currentCategory();
  if (!cat) {
    alert("Категория не загружена. Обновите страницу и повторите попытку.");
    return;
  }
  const isEdit = !!entry;
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

  const backdrop =
    el(`<div class="modal-backdrop"><div class="modal" role="dialog" aria-modal="true">
    <form id="m-form" novalidate>
    <h2 style="margin-top:0">${isEdit ? "Редактировать запись" : "Новая запись"} — ${escapeHTML(cat.name)}</h2>
    <div class="field"><label>Аудитория</label>
      <select id="m-audience" disabled><option value="${escapeHTML(audience)}">${escapeHTML(AUDIENCE_LABELS[audience])}</option></select>
    </div>
    <div class="field"><label>Соглашение</label><input value="${escapeHTML(agreementLabel(agreement))}" disabled></div>
    <div id="m-fields"></div>
    ${
      isEdit
        ? `<div class="field"><label>Комментарий к изменению (обязателен)</label><textarea id="m-comment" rows="2"></textarea></div>`
        : ""
    }
    <div class="error" id="m-error" style="display:none"></div>
    <div class="flex between" style="margin-top:14px">
      <div>${entry ? `<span class="muted">Текущая сумма: ${fmtMoney(entry.amount_rub)}</span>` : ""}</div>
      <div class="flex">
        <button type="button" class="btn secondary" id="m-cancel">Отмена</button>
        <button type="submit" class="btn" id="m-save">${isEdit ? "Сохранить" : "Создать"}</button>
      </div>
    </div>
    ${renderAttachSection()}
    </form>
  </div></div>`);

  const fieldsBox = backdrop.querySelector("#m-fields");
  cat.fields.forEach((f) => {
    const row = el(
      `<div class="field"><label>${escapeHTML(f.label)}${f.required ? " *" : ""}</label><div class="field-error" style="display:none"></div></div>`,
    );
    row.insertBefore(
      el(fieldInput(f, payload[f.key], audience)),
      row.querySelector(".field-error"),
    );
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
    if (f.key === "org_name") row.querySelector("select").disabled = true;
    if (f.key === "mentor_full_name") row.hidden = true;
    if (f.key === "mentor_id") {
      const select = row.querySelector("select");
      select.onchange = () => {
        fieldsBox.querySelector('[data-key="mentor_full_name"]').value =
          state.mentors.find((m) => m.id === select.value)?.full_name || "";
      };
      const add = el(
        '<button type="button" class="btn secondary">+ Наставник</button>',
      );
      add.onclick = async () => {
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
      row.appendChild(add);
    }
  });

  const closeModal = () => {
    if (busy) return;
    document.removeEventListener("keydown", closeOnEscape);
    backdrop.remove();
  };
  const closeOnEscape = (event) => {
    if (event.key === "Escape") closeModal();
  };
  backdrop.querySelector("#m-cancel").onclick = closeModal;
  backdrop.onclick = (event) => {
    if (event.target === backdrop) closeModal();
  };
  document.addEventListener("keydown", closeOnEscape);
  backdrop.querySelector("#m-form").onsubmit = async (event) => {
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
      validateFileBatch(backdrop.querySelector("#attach-file").files);
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
          }),
        });
        savedID = created.id;
      }
      fieldsBox
        .querySelectorAll("input,select,button")
        .forEach((input) => (input.disabled = true));
      await uploadFileBatch(
        savedID,
        backdrop.querySelector("#attach-file").files,
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

  document.body.appendChild(backdrop);

  if (isEdit) {
    wireAttachSection(backdrop, entry.id);
  } else {
    backdrop.querySelector("#attach-list").textContent =
      "Файлы необязательны. Выбранные файлы загрузятся после создания записи.";
    backdrop.querySelector("#attach-upload").hidden = true;
  }
}

function renderAttachSection() {
  return `<div class="card" style="margin-top:14px;background:transparent;padding:0;border:none">
    <h2>Вложения — необязательно</h2><p class="muted">До 20 файлов за раз, суммарно до 64 МБ. Можно сохранить запись без файлов.</p>
    <div id="attach-list" class="attach-list muted">Загрузка…</div>
    <div class="field" style="margin-top:8px">
      <input type="file" id="attach-file" multiple aria-label="Необязательные вложения">
      <button type="button" class="btn secondary" id="attach-upload">Загрузить</button>
    </div>
  </div>`;
}

async function wireAttachSection(root, entryId) {
  const list = root.querySelector("#attach-list");
  const refresh = async () => {
    try {
      const items = (await api(`/entries/${entryId}/attachments`)) || [];
      list.innerHTML = items.length
        ? items
            .map(
              (a) =>
                `<div>📄 <a href="/api/attachments/${encodeURIComponent(a.id)}/download">${escapeHTML(a.file_name)}</a> <span class="muted">(до ${new Date(
                  a.retention_expires_at,
                ).toLocaleDateString("ru-RU")})</span></div>`,
            )
            .join("")
        : `<span class="muted">Файлов пока нет</span>`;
    } catch (e) {
      list.innerHTML = `<span class="error">${escapeHTML(e.message)}</span>`;
    }
  };
  root.querySelector("#attach-upload").onclick = async () => {
    const fileInput = root.querySelector("#attach-file");
    const uploadButton = root.querySelector("#attach-upload");
    if (!fileInput.files.length) {
      showToast("Сначала выберите файл");
      return;
    }
    uploadButton.disabled = true;
    uploadButton.textContent = "Загружаем…";
    try {
      await uploadFileBatch(entryId, fileInput.files);
      fileInput.value = "";
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

// ----------------------------------------------------------------- ADMIN --

async function renderAdmin(root) {
  const isAdmin = state.me.role === "admin";
  const showEducationDirectory = canReviewEducationDirectory();
  const showITDirectory = canViewITCompanies();
  const initialDirectoryTab = showEducationDirectory ? "partners" : "it-companies";
  root.innerHTML = `<section class="page-heading"><div><h1>Управление</h1></div></section>
  <div class="admin-layout">
    <nav class="admin-nav" aria-label="Разделы административной панели">
      ${showEducationDirectory ? `<button data-t="partners"${initialDirectoryTab === "partners" ? ' class="active"' : ""}><b>Справочник ОО</b></button>` : ""}
      ${showITDirectory ? `<button data-t="it-companies"${initialDirectoryTab === "it-companies" ? ' class="active"' : ""}><b>ИТ-компании</b></button>` : ""}
      ${isAdmin ? '<button data-t="users"><b>Пользователи</b></button><button data-t="settings"><b>Настройки</b></button><button data-t="logs"><b>Журнал изменений</b></button>' : ""}
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
    const adminOnly = new Set(["users", "settings", "logs"]);
    if (adminOnly.has(tab) && state.me.role !== "admin")
      throw new Error("Этот раздел доступен только администратору");
    if (tab === "users") return await renderAdminUsers(box);
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
      <div class="field"><label>Email *</label><input id="u-email" type="email" maxlength="254" autocomplete="off" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Пароль *</label><input id="u-password" type="password" minlength="10" maxlength="128" autocomplete="new-password" required><div class="field-hint">10–128 символов: A–Z, a–z, цифра и спецсимвол</div><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>ФИО *</label><input id="u-name" minlength="2" maxlength="200" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Роль</label><select id="u-role"><option value="user">Пользователь</option><option value="moderator">Модератор</option><option value="admin">Администратор</option></select><div class="field-hint">Модератор не управляет пользователями и настройками.</div></div>
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
    box.querySelector("#u-partner-field").hidden = !education;
    box.querySelector("#u-company-field").hidden = education;
    const companyRequired = roleSelect.value === "user";
    box.querySelector("#u-partner-label").textContent = "Учебное заведение *";
    box.querySelector("#u-company-label").textContent = `ИТ-компания${companyRequired ? " *" : ""}`;
    if (!education)
      box.querySelector("#u-partner").value = "";
    else box.querySelector("#u-company").value = "";
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
      roleSelect.value === "user"
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
        ? `<div class="table-wrap"><table><thead><tr><th>Email</th><th>ФИО</th><th>Организация</th><th>Роль</th><th>Статус</th><th></th></tr></thead>
          <tbody>${users
            .map(
              (u) => `<tr>
            <td>${escapeHTML(u.email)}</td><td>${escapeHTML(u.full_name)}</td><td>${escapeHTML(u.entity_type === "edu_institution" ? state.partners.find((partner) => partner.id === u.partner_id)?.name || "Учебное заведение не назначено" : state.itCompanies.find((company) => company.id === u.it_company_id)?.name || "ИТ-компания не назначена")}</td><td><span class="role-badge">${escapeHTML(valueLabel(u.role))}</span></td>
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
  box.innerHTML = `<div class="card"><h2>Настройки хранения</h2>
    <div class="grid cols-2">
      <div class="field"><label>Хранение подтверждающих документов, дней</label>
        <input id="s-attach" type="number" min="1" max="3650" step="1" value="${settings.attachment_retention_days || 365}"></div>
      <div class="field"><label>Хранение журнала изменений, дней</label>
        <input id="s-audit" type="number" min="60" max="3650" step="1" value="${settings.audit_log_retention_days || 60}"></div>
    </div>
    <button class="btn" id="s-save">Сохранить</button>
    <p class="field-hint">Журнал изменений хранится не менее 60 дней.</p>
  </div>`;
  box.querySelector("#s-save").onclick = async () => {
    const button = box.querySelector("#s-save");
    const attachmentDays = Number(box.querySelector("#s-attach").value);
    const auditDays = Number(box.querySelector("#s-audit").value);
    if (auditDays < 60) { showToast("Журнал аудита хранится минимум 60 дней"); return; }
    if (
      ![attachmentDays, auditDays].every(
        (value) => Number.isInteger(value) && value >= 1 && value <= 3650,
      )
    ) {
      showToast("Срок хранения должен быть целым числом от 1 до 3650 дней");
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
  };
  const summarize = (log) => {
    const before = log.old_value || {};
    const after = log.new_value || {};
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
        <td>${escapeHTML(log.user_name || (log.user_id ? "Пользователь" : "Автоматический скрипт"))}${log.user_email ? `<br><small>${escapeHTML(log.user_email)}</small>` : ""}</td>
        <td>${escapeHTML(valueLabel(log.entity_type))}${log.entity_id ? " #" + escapeHTML(log.entity_id.slice(0, 8)) : ""}<br><b>${escapeHTML(valueLabel(log.action))}</b></td>
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

boot();
