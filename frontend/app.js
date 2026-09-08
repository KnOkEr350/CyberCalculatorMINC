// Калькулятор затрат по Приказу Минцифры — фронтенд.
// Без фреймворков и сборки: чистый fetch + DOM. Один файл, простой роутер по хэшу.

const state = {
  me: null,
  categories: [],
  partners: [],
  view: "dashboard",
  period: "plan",
  year: new Date().getFullYear(),
  categoryCode: null,
  entries: [],
  dashboard: null,
  categoriesError: null,
};

const CATEGORY_LABELS = {}; // заполняется из /api/categories
const AUDIENCE_LABELS = { vuz: "Вуз", kolledj: "СПО", school: "Школьный трек" };
const CHART_COLORS = ["#1a79ff", "#00a6a6", "#6757d9", "#f2a51a", "#e85c8b", "#36a269", "#489dff", "#805ad5", "#de6f3c"];
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
  admin: "Администратор",
  organization: "ИТ-компания",
  edu_institution: "Образовательная организация",
  entry: "Запись",
  attachment: "Документ",
  partner: "Партнёр",
  settings: "Настройки",
  create: "Создание",
  upload: "Загрузка файла",
  login: "Вход",
  delete: "Удаление",
  settings_change: "Изменение настроек",
};

async function api(path, opts = {}) {
  const res = await fetch("/api" + path, {
    credentials: "same-origin",
    headers: opts.body && !(opts.body instanceof FormData) ? { "Content-Type": "application/json" } : undefined,
    ...opts,
  });
  if (res.status === 401 && path !== "/auth/login" && path !== "/auth/me") {
    state.me = null;
    render();
    throw new Error("требуется авторизация");
  }
  const isJSON = (res.headers.get("content-type") || "").includes("application/json");
  const data = isJSON ? await res.json().catch(() => null) : null;
  if (!res.ok) {
    throw new Error((data && data.error) || `Ошибка ${res.status}`);
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
  return Number(v || 0).toLocaleString("ru-RU", { maximumFractionDigits: 2 }) + " ₽";
}

function valueLabel(value) {
  return VALUE_LABELS[value] || value;
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
  const toast = el(`<div class="toast ${escapeHTML(kind)}" role="status"></div>`);
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
  if (!state.me.entity_type) {
    app.appendChild(renderChooseEntity());
    return;
  }
  app.appendChild(renderLayout());
}

// ----------------------------------------------------------------- LOGIN --

function renderLogin() {
  const wrap = el(`<div class="auth-shell">
    <aside class="auth-brand-panel">
      ${brandMarkup(true)}
      <div class="auth-message">
        <span class="eyebrow">Контроль исполнения</span>
        <h1>Планируйте затраты.<br>Подтверждайте результат.</h1>
        <p>Единое пространство для расчёта мероприятий по Приказу Минцифры, ведения плана и фиксации факта.</p>
      </div>
      <div class="auth-orbit" aria-hidden="true"><i></i><i></i><i></i></div>
    </aside>
    <main class="auth-form-panel">
      <form class="login-box" id="login-form">
        <span class="eyebrow">Личный кабинет</span>
        <h2>Вход в систему</h2>
        <p class="muted">Используйте учётную запись, выданную администратором.</p>
        <div class="field"><label for="login-email">Email</label><input type="email" id="login-email" autocomplete="username" required placeholder="name@company.ru"></div>
        <div class="field"><label for="login-password">Пароль</label><input type="password" id="login-password" autocomplete="current-password" required placeholder="Введите пароль"></div>
        <div class="error form-message" id="login-error" style="display:none" role="alert"></div>
        <button type="submit" class="btn wide" id="login-submit">Войти</button>
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
      await api("/auth/login", { method: "POST", body: JSON.stringify({ email, password }) });
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

function renderChooseEntity() {
  const wrap = el(`<div class="auth-shell compact">
    <aside class="auth-brand-panel">
      ${brandMarkup(true)}
      <div class="auth-message"><span class="eyebrow">Первый вход</span><h1>Настроим рабочее пространство</h1><p>Тип организации определяет доступные сценарии расчёта и отображение отчётов.</p></div>
      <div class="auth-orbit" aria-hidden="true"><i></i><i></i><i></i></div>
    </aside>
    <main class="auth-form-panel"><div class="login-box entity-choice">
      <span class="eyebrow">Профиль</span><h2>Кого вы представляете?</h2>
      <p class="muted">Выберите подходящий вариант. При необходимости администратор сможет уточнить настройки.</p>
      <button type="button" class="choice-card" id="pick-org"><b>Организация</b><span>ИТ-компания, ведущая план и факт затрат</span><i>→</i></button>
      <div class="field entity-partner-field"><label for="entity-partner">Образовательная организация</label><select id="entity-partner"><option value="">Выберите организацию</option>${state.partners
        .map((partner) => `<option value="${escapeHTML(partner.id)}">${escapeHTML(partner.name)}</option>`)
        .join("")}</select></div>
      <button type="button" class="choice-card" id="pick-edu"><b>Вуз, колледж или школа</b><span>Образовательная организация — партнёр</span><i>→</i></button>
      <div class="error form-message" id="entity-error" style="display:none" role="alert"></div>
    </div></main>
  </div>`);
  const pick = async (entity_type, partner_id = null) => {
    const buttons = wrap.querySelectorAll(".choice-card");
    const error = wrap.querySelector("#entity-error");
    buttons.forEach((button) => (button.disabled = true));
    error.style.display = "none";
    try {
      await api("/auth/entity-type", { method: "POST", body: JSON.stringify({ entity_type, partner_id }) });
      await boot();
    } catch (e) {
      error.textContent = e.message;
      error.style.display = "block";
      buttons.forEach((button) => (button.disabled = false));
    }
  };
  wrap.querySelector("#pick-org").onclick = () => pick("organization");
  wrap.querySelector("#pick-edu").onclick = () => {
    const partnerID = wrap.querySelector("#entity-partner").value;
    if (!partnerID) {
      const error = wrap.querySelector("#entity-error");
      error.textContent = "Выберите свою образовательную организацию";
      error.style.display = "block";
      return;
    }
    pick("edu_institution", partnerID);
  };
  return wrap;
}

// ---------------------------------------------------------------- LAYOUT --

function renderLayout() {
  const isAdmin = state.me.role === "admin";
  const wrap = el(`<div>
    <div class="topbar">
      ${brandMarkup()}
      <nav>
        <button data-view="dashboard">Дашборд</button>
        <button data-view="entries">План / Факт</button>
        ${isAdmin ? '<button data-view="admin">Админка</button>' : ""}
      </nav>
      <div class="who">
        <span class="avatar">${escapeHTML(initials(state.me.full_name))}</span>
        <span class="user-copy"><strong>${escapeHTML(state.me.full_name)}</strong><small>${escapeHTML(valueLabel(state.me.entity_type))}${isAdmin ? " · Администратор" : ""}</small></span>
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
  const content = wrap.querySelector("#content");
  if (state.view === "dashboard") renderDashboard(content);
  else if (state.view === "entries") renderEntries(content);
  else if (state.view === "admin") renderAdmin(content);
  return wrap;
}

// ------------------------------------------------------------- DASHBOARD --

async function renderDashboard(root) {
  root.appendChild(el(`<div class="muted">Загрузка дашборда…</div>`));
  let d;
  try {
    d = await api(`/dashboard?report_year=${state.year}`);
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
      </div>`
          )
          .join("")
      : `<div class="muted">Нет данных за ${state.year} год</div>`;

  const groupedChart = (plan, fact) => {
    const planMap = new Map((plan || []).map((item) => [item.category_code, Number(item.amount_rub) || 0]));
    const factMap = new Map((fact || []).map((item) => [item.category_code, Number(item.amount_rub) || 0]));
    const codes = [...new Set([...planMap.keys(), ...factMap.keys()])];
    if (!codes.length) return `<div class="chart-empty">Добавьте записи плана или факта — здесь появится сравнение.</div>`;
    const max = Math.max(1, ...codes.flatMap((code) => [planMap.get(code) || 0, factMap.get(code) || 0]));
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
          (item, index) => `<div><i style="background:${CHART_COLORS[index % CHART_COLORS.length]}"></i><span>${escapeHTML(
            CATEGORY_LABELS[item.category_code] || item.category_code
          )}</span><b>${Number(item.share_percent || 0).toLocaleString("ru-RU")}%</b></div>`
        )
        .join("")}</div>
    </div>`;
  };

  const planPct = clampPercent(d.plan_completion_pct);
  const targetPct = d.target_amount_rub ? clampPercent((Number(d.fact_total_rub) / Number(d.target_amount_rub)) * 100) : 0;

  root.innerHTML = `
    <section class="page-heading">
      <div><span class="eyebrow">Аналитика</span><h1>Дашборд</h1><p>Сводная картина исполнения обязательств за выбранный отчётный год.</p></div>
      <span class="year-badge">${state.year}</span>
    </section>
    <div class="card">
      <div class="flex between">
        <h2 style="margin:0">Пульс проекта</h2>
        <div class="flex">
          <label style="margin:0">Год</label>
          <input type="number" id="dash-year" min="2000" max="2100" step="1" value="${state.year}" style="width:90px">
          ${
            state.me.role === "admin"
              ? `<button class="btn secondary" id="set-target">Задать целевую сумму (3%)</button>`
              : ""
          }
        </div>
      </div>
      <div class="grid cols-3" style="margin-top:14px">
        <div class="stat"><div class="label">Целевая сумма (3% от льгот)</div><div class="value">${
          d.target_amount_rub != null ? fmtMoney(d.target_amount_rub) : "не задана"
        }</div></div>
        <div class="stat"><div class="label">План, руб.</div><div class="value">${fmtMoney(d.plan_total_rub)}</div></div>
        <div class="stat"><div class="label">Факт, руб.</div><div class="value">${fmtMoney(d.fact_total_rub)}</div></div>
      </div>
      <div class="progress-card" style="margin-top:10px">
        <div class="progress-header"><span>Реализация плана</span><strong>${Number(d.plan_completion_pct || 0).toLocaleString("ru-RU")}%</strong></div>
        <div class="progress-track"><div class="progress-fill" style="width:${planPct}%"></div></div>
        ${
          d.target_amount_rub
            ? `<div class="progress-header target"><span>Выполнение минимального объёма (3%)</span><strong>${Math.round(
                (Number(d.fact_total_rub) / Number(d.target_amount_rub)) * 10000
              ) / 100}%</strong></div><div class="progress-track"><div class="progress-fill target" style="width:${targetPct}%"></div></div>`
            : ""
        }
      </div>
    </div>
    <div class="card"><h2>План и факт по категориям</h2>${groupedChart(d.plan_by_category, d.fact_by_category)}</div>
    <div class="grid cols-2">
      <div class="card"><h2>Диаграмма структуры — План</h2>${donutChart(d.plan_by_category, d.plan_total_rub, "План")}</div>
      <div class="card"><h2>Диаграмма структуры — Факт</h2>${donutChart(d.fact_by_category, d.fact_total_rub, "Факт")}</div>
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
  const targetBtn = root.querySelector("#set-target");
  if (targetBtn) {
    targetBtn.onclick = async () => {
      const val = prompt("Целевая сумма затрат на " + state.year + " год, руб. (3% от сэкономленных льгот):", d.target_amount_rub || "");
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
          body: JSON.stringify({ report_year: state.year, target_amount_rub: amount }),
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
  if (!state.categories.length) {
    root.innerHTML = `<div class="card muted">Загрузка справочника категорий…</div>`;
    try {
      state.categories = (await api("/categories")) || [];
      state.categories.forEach((c) => (CATEGORY_LABELS[c.code] = c.name));
      state.categoriesError = null;
    } catch (e) {
      state.categoriesError = e.message;
      root.innerHTML = `<div class="card"><div class="error">Не удалось загрузить категории: ${escapeHTML(
        e.message
      )}</div><button type="button" class="btn secondary" id="retry-categories">Повторить</button></div>`;
      root.querySelector("#retry-categories").onclick = () => renderEntries(root);
      return;
    }
  }
  if (!state.categories.length) {
    root.innerHTML = `<div class="card"><div class="error">Справочник категорий пуст. Добавление записи недоступно.</div></div>`;
    return;
  }
  if (!state.categoryCode || !state.categories.some((category) => category.code === state.categoryCode)) {
    state.categoryCode = state.categories[0].code;
  }

  root.innerHTML = `<section class="page-heading">
    <div><span class="eyebrow">Отчётность</span><h1>План и факт</h1><p>Добавляйте мероприятия и отслеживайте подтверждённые затраты.</p></div>
    <span class="year-badge">${state.year}</span>
  </section>
  <div class="card filter-card">
    <div class="tabs">
      <button data-p="plan" class="${state.period === "plan" ? "active" : ""}">План</button>
      <button data-p="fact" class="${state.period === "fact" ? "active" : ""}">Факт</button>
    </div>
    <div class="grid cols-3">
      <div class="field"><label>Год</label><input type="number" id="year" min="2000" max="2100" step="1" value="${state.year}"></div>
      <div class="field"><label>Категория активности</label>
        <select id="category">${state.categories
          .map((c) => `<option value="${escapeHTML(c.code)}" ${c.code === state.categoryCode ? "selected" : ""}>${escapeHTML(c.name)}</option>`)
          .join("")}</select>
      </div>
      <div class="field right" style="align-self:end">
        <button type="button" class="btn" id="add-entry">+ Добавить запись</button>
        <a class="btn secondary" id="export-link" href="#" target="_blank" style="text-decoration:none;display:inline-block">Выгрузить категорию</a>
        <a class="btn secondary" id="export-all-link" href="#" target="_blank" style="text-decoration:none;display:inline-block">Выгрузить весь год</a>
      </div>
    </div>
  </div>
  <div class="card"><h2>Записи</h2><div id="entries-table">Загрузка…</div></div>`;

  root.querySelectorAll(".tabs button").forEach((b) => {
    b.onclick = () => {
      state.period = b.dataset.p;
      renderEntries(root);
    };
  });
  root.querySelector("#year").onchange = (e) => {
    const year = Number(e.target.value);
    if (validYear(year)) {
      state.year = year;
      renderEntries(root);
    } else {
      e.target.reportValidity();
    }
  };
  root.querySelector("#category").onchange = (e) => {
    state.categoryCode = e.target.value;
    renderEntries(root);
  };
  root.querySelector("#export-link").href = `/api/reports/export?period_type=${state.period}&report_year=${state.year}&category_code=${state.categoryCode}`;
  root.querySelector("#export-all-link").href = `/api/reports/export?period_type=${state.period}&report_year=${state.year}`;
  root.querySelector("#add-entry").onclick = () => openEntryModal(null);

  await loadEntriesTable(root);
}

async function loadEntriesTable(root) {
  const box = root.querySelector("#entries-table");
  try {
    const list = await api(
      `/entries?category_code=${encodeURIComponent(state.categoryCode)}&period_type=${state.period}&report_year=${state.year}`
    );
    state.entries = list || [];
  } catch (e) {
    box.innerHTML = `<div class="error">${escapeHTML(e.message)}</div>`;
    return;
  }
  if (!state.entries.length) {
    box.innerHTML = `<div class="muted">Записей пока нет</div>`;
    return;
  }
  const total = state.entries.reduce((s, e) => s + Number(e.amount_rub), 0);
  box.innerHTML = `<div class="table-wrap"><table>
    <thead><tr><th>Партнёр</th><th>Аудитория</th><th>Сумма, руб.</th><th></th></tr></thead>
    <tbody>
      ${state.entries
        .map(
          (e) => `<tr data-id="${escapeHTML(e.id)}">
        <td>${escapeHTML(partnerName(e.partner_id))}</td>
        <td>${escapeHTML(valueLabel(e.audience))}</td>
        <td>${escapeHTML(fmtMoney(e.amount_rub))}</td>
        <td class="muted">ред.</td>
      </tr>`
        )
        .join("")}
    </tbody>
    <tfoot><tr><td colspan="2"><b>Итого</b></td><td><b>${fmtMoney(total)}</b></td><td></td></tr></tfoot>
  </table></div>`;
  box.querySelectorAll("tbody tr").forEach((tr) => {
    tr.onclick = () => {
      const entry = state.entries.find((e) => e.id === tr.dataset.id);
      openEntryModal(entry);
    };
  });
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
            .filter((p) => (!audience || p.partner_kind === audience) && (!state.me.partner_id || p.id === state.me.partner_id))
            .map((p) => ({ v: p.id, l: p.name }))
        : (f.options || []).map((o) => ({ v: o, l: valueLabel(o) }));
    return `<select data-key="${escapeHTML(f.key)}" data-kind="select" ${f.required ? "required" : ""}>
      <option value="">—</option>
      ${opts
        .map((o) => `<option value="${escapeHTML(o.v)}" ${o.v === val ? "selected" : ""}>${escapeHTML(o.l)}</option>`)
        .join("")}
    </select>`;
  }
  if (f.type === "number") {
    return `<input type="number" min="0" max="1000000000000" step="${f.integer ? "1" : "any"}" data-key="${escapeHTML(
      f.key
    )}" data-kind="number" value="${escapeHTML(val)}" ${f.required ? "required" : ""}>`;
  }
  return `<input type="text" maxlength="${f.max_length || 1000}" data-key="${escapeHTML(f.key)}" data-kind="text" value="${escapeHTML(
    val
  )}" ${f.required ? "required" : ""}>`;
}

function clearFieldErrors(container) {
  container.querySelectorAll(".invalid").forEach((input) => input.classList.remove("invalid"));
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
    const input = fieldsBox.querySelector(`[data-key="${CSS.escape(field.key)}"]`);
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
        else if (number > 1_000_000_000_000) message = "Значение слишком велико";
        else if (field.integer && !Number.isInteger(number)) message = "Введите целое число";
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
  if (category.code === "teachers") requirePositive("academic_hours", "Количество часов должно быть больше нуля");
  if (category.code === "internship" || category.code === "employment_practice") {
    requirePositive("duration_months", "Продолжительность должна быть больше нуля");
    if (!(Number(payload.student_load_hours_per_month) > 0 || Number(payload.mentor_load_hours_per_month) > 0)) {
      const input = fieldsBox.querySelector('[data-key="student_load_hours_per_month"]');
      if (input) {
        setFieldError(input, "Укажите нагрузку студента или наставника больше нуля");
        firstInvalid ||= input;
      }
    }
  }
  if (category.code === "top_it") requirePositive("cofinancing_amount_rub", "Сумма должна быть больше нуля");
  if (category.code === "minc_decision") requirePositive("amount_manual", "Сумма должна быть больше нуля");
  if (category.code === "it_clubs" && !(Number(payload.academic_hours) > 0 || Number(payload.developed_programs_count) > 0)) {
    const input = fieldsBox.querySelector('[data-key="academic_hours"]');
    if (input) {
      setFieldError(input, "Укажите часы или количество разработанных программ");
      firstInvalid ||= input;
    }
  }
  if (
    category.code === "teacher_training" &&
    !(Number(payload.developed_programs_count) > 0 || (Number(payload.academic_hours_per_teacher) > 0 && Number(payload.trained_teachers_count) > 0))
  ) {
    const input = fieldsBox.querySelector('[data-key="developed_programs_count"]');
    if (input) {
      setFieldError(input, "Укажите разработанную программу либо часы и число обученных учителей");
      firstInvalid ||= input;
    }
  }
  if (category.code === "edu_content" && !(Number(payload.student_platform_months) > 0 || Number(payload.teacher_platform_months) > 0)) {
    const input = fieldsBox.querySelector('[data-key="student_platform_months"]');
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
  const payload = entry ? entry.payload || {} : {};
  const audience = entry ? entry.audience : cat.audience_scope[0];

  const backdrop = el(`<div class="modal-backdrop"><div class="modal" role="dialog" aria-modal="true">
    <form id="m-form" novalidate>
    <h2 style="margin-top:0">${isEdit ? "Редактировать запись" : "Новая запись"} — ${escapeHTML(cat.name)}</h2>
    <div class="field"><label>Аудитория</label>
      <select id="m-audience">${cat.audience_scope.map((a) => `<option value="${escapeHTML(a)}" ${a === audience ? "selected" : ""}>${escapeHTML(AUDIENCE_LABELS[a] || a)}</option>`).join("")}</select>
    </div>
    <div id="m-fields"></div>
    ${
      isEdit
        ? `<div class="field"><label>Комментарий к изменению (обязателен)</label><textarea id="m-comment" rows="2"></textarea></div>`
        : ""
    }
    ${state.period === "fact" ? renderAttachSection(isEdit) : ""}
    <div class="error" id="m-error" style="display:none"></div>
    <div class="flex between" style="margin-top:14px">
      <div>${entry ? `<span class="muted">Текущая сумма: ${fmtMoney(entry.amount_rub)}</span>` : ""}</div>
      <div class="flex">
        <button type="button" class="btn secondary" id="m-cancel">Отмена</button>
        <button type="submit" class="btn" id="m-save">${isEdit ? "Сохранить" : "Создать"}</button>
      </div>
    </div>
    </form>
  </div></div>`);

  const fieldsBox = backdrop.querySelector("#m-fields");
  cat.fields.forEach((f) => {
    const row = el(`<div class="field"><label>${escapeHTML(f.label)}${f.required ? " *" : ""}</label><div class="field-error" style="display:none"></div></div>`);
    row.insertBefore(el(fieldInput(f, payload[f.key], audience)), row.querySelector(".field-error"));
    if (f.key === "org_name") {
      const hasPartners =
        Array.isArray(state.partners) &&
        state.partners.some((partner) => partner.partner_kind === audience && (!state.me.partner_id || partner.id === state.me.partner_id));
      row.appendChild(el(`<div class="field-hint partner-hint"${hasPartners ? ' style="display:none"' : ""}>Для этой аудитории пока нет партнёров. Попросите администратора добавить организацию.</div>`));
    }
    fieldsBox.appendChild(row);
  });

  const closeModal = () => {
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
    const validation = collectAndValidateEntryPayload(fieldsBox, cat);
    if (!validation.valid) return;
    const newPayload = validation.payload;
    const aud = backdrop.querySelector("#m-audience").value;
    const errBox = backdrop.querySelector("#m-error");
    const saveButton = backdrop.querySelector("#m-save");
    errBox.style.display = "none";
    saveButton.disabled = true;
    saveButton.textContent = isEdit ? "Сохраняем…" : "Создаём…";
    try {
      const pendingFile = backdrop.querySelector("#attach-file")?.files[0];
      if (isEdit) {
        const comment = backdrop.querySelector("#m-comment").value.trim();
        if (!comment) throw new Error("Комментарий обязателен при редактировании");
        if (state.period === "fact" && pendingFile) {
          await uploadAttachment(entry.id, pendingFile);
        }
        await api(`/entries/${entry.id}`, {
          method: "PUT",
          body: JSON.stringify({ payload: newPayload, audience: aud, comment }),
        });
      } else {
        const partnerId = newPayload.org_name || null;
        const entryData = {
          category_code: state.categoryCode,
          partner_id: partnerId,
          period_type: state.period,
          report_year: state.year,
          audience: aud,
          payload: newPayload,
        };
        let body = JSON.stringify(entryData);
        if (state.period === "fact") {
          if (!pendingFile) throw new Error("Для фактической записи прикрепите подтверждающий документ");
          body = new FormData();
          body.append("entry", JSON.stringify(entryData));
          body.append("file", pendingFile);
        }
        await api(`/entries`, { method: "POST", body });
      }
      closeModal();
      const root = document.getElementById("content");
      if (root) await loadEntriesTable(root);
    } catch (e) {
      errBox.textContent = e.message;
      errBox.style.display = "block";
    } finally {
      saveButton.disabled = false;
      saveButton.textContent = isEdit ? "Сохранить" : "Создать";
    }
  };

  backdrop.querySelector("#m-audience").onchange = (event) => {
    const partnerSelect = fieldsBox.querySelector('[data-key="org_name"]');
    if (!partnerSelect) return;
    const selected = partnerSelect.value;
    const options = (Array.isArray(state.partners) ? state.partners : []).filter(
      (partner) => partner.partner_kind === event.target.value && (!state.me.partner_id || partner.id === state.me.partner_id)
    );
    partnerSelect.innerHTML = `<option value="">—</option>${options
      .map((partner) => `<option value="${escapeHTML(partner.id)}">${escapeHTML(partner.name)}</option>`)
      .join("")}`;
    const partnerHint = partnerSelect.closest(".field")?.querySelector(".partner-hint");
    if (partnerHint) partnerHint.style.display = options.length ? "none" : "block";
    if (options.some((partner) => partner.id === selected)) partnerSelect.value = selected;
    clearFieldErrors(fieldsBox);
  };

  document.body.appendChild(backdrop);

  if (isEdit && entry.period_type === "fact") {
    wireAttachSection(backdrop, entry.id);
  }
}

function renderAttachSection(isEdit) {
  return `<div class="attachment-panel">
    <h2>Подтверждающий документ${isEdit ? "" : " *"}</h2>
    <p class="muted">Для фактической записи документ обязателен. Максимальный размер — 64 МБ.</p>
    <div id="attach-list" class="attach-list muted">${isEdit ? "Загрузка…" : "Файл будет загружен вместе с записью"}</div>
    <div class="file-row">
      <input type="file" id="attach-file" ${isEdit ? "" : "required"}>
      ${isEdit ? '<button type="button" class="btn secondary" id="attach-upload">Загрузить</button>' : ""}
    </div>
  </div>`;
}

async function uploadAttachment(entryId, file) {
  const form = new FormData();
  form.append("file", file);
  return api(`/entries/${encodeURIComponent(entryId)}/attachments`, { method: "POST", body: form });
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
                  a.retention_expires_at
                ).toLocaleDateString("ru-RU")})</span></div>`
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
      await uploadAttachment(entryId, fileInput.files[0]);
      fileInput.value = "";
      await refresh();
      showToast("Документ загружен", "success");
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
  root.innerHTML = `<section class="page-heading">
    <div><span class="eyebrow">Управление</span><h1>Администрирование</h1><p>Пользователи, партнёры, сроки хранения и журнал действий.</p></div>
  </section>
  <div class="tabs admin-tabs">
    <button data-t="users" class="active">Пользователи</button>
    <button data-t="partners">Партнёры</button>
    <button data-t="settings">Настройки</button>
    <button data-t="logs">Журнал изменений</button>
  </div><div id="admin-content"></div>`;
  const box = root.querySelector("#admin-content");
  root.querySelectorAll(".tabs button").forEach((b) => {
    b.onclick = () => {
      root.querySelectorAll(".tabs button").forEach((x) => x.classList.remove("active"));
      b.classList.add("active");
      renderAdminTab(box, b.dataset.t);
    };
  });
  renderAdminTab(box, "users");
}

async function renderAdminTab(box, tab) {
  box.innerHTML = `<div class="card loading-state"><span class="spinner"></span>Загрузка данных…</div>`;
  try {
    if (tab === "users") return await renderAdminUsers(box);
    if (tab === "partners") return await renderAdminPartners(box);
    if (tab === "settings") return await renderAdminSettings(box);
    if (tab === "logs") return await renderAdminLogs(box);
    throw new Error("Неизвестный раздел администрирования");
  } catch (e) {
    box.innerHTML = `<div class="card error-state"><b>Не удалось загрузить раздел</b><span>${escapeHTML(e.message)}</span><button type="button" class="btn secondary" id="admin-retry">Повторить</button></div>`;
    box.querySelector("#admin-retry").onclick = () => renderAdminTab(box, tab);
  }
}

async function renderAdminUsers(box) {
  box.innerHTML = `<div class="card"><h2>Новый пользователь (в т.ч. дополнительный админ)</h2>
    <form id="u-form" novalidate>
    <div class="grid cols-3">
      <div class="field"><label>Электронная почта *</label><input id="u-email" type="email" maxlength="254" autocomplete="off" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Пароль *</label><input id="u-password" type="password" minlength="10" maxlength="128" autocomplete="new-password" required><div class="field-hint">10–128 символов: заглавная и строчная буквы, цифра и специальный символ</div><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>ФИО *</label><input id="u-name" minlength="2" maxlength="200" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Роль</label><select id="u-role"><option value="user">Пользователь</option><option value="admin">Администратор</option></select></div>
      <div class="field"><label>Тип пользователя</label><select id="u-entity"><option value="">Выберет при первом входе</option><option value="organization">ИТ-компания</option><option value="edu_institution">Образовательная организация</option></select></div>
      <div class="field" id="u-partner-field" style="display:none"><label>Партнёр</label><select id="u-partner"><option value="">Не назначен</option>${(Array.isArray(state.partners) ? state.partners : [])
        .map((partner) => `<option value="${escapeHTML(partner.id)}">${escapeHTML(partner.name)}</option>`)
        .join("")}</select></div>
    </div>
    <button type="submit" class="btn" id="u-create">Создать</button>
    <div class="error" id="u-error" style="display:none"></div>
    </form>
  </div>
  <div class="card"><h2>Пользователи</h2><div id="u-list">Загрузка…</div></div>`;

  const entitySelect = box.querySelector("#u-entity");
  entitySelect.onchange = () => {
    box.querySelector("#u-partner-field").style.display = entitySelect.value === "edu_institution" ? "block" : "none";
    if (entitySelect.value !== "edu_institution") box.querySelector("#u-partner").value = "";
  };

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
    if (!email || !emailInput.validity.valid || email.length > 254) mark(emailInput, "Введите корректный email");
    if (fullName.length < 2 || fullName.length > 200 || !/\p{L}/u.test(fullName)) mark(nameInput, "Введите ФИО от 2 до 200 символов");
    if (password.length < 10 || password.length > 128) mark(passwordInput, "Пароль должен содержать от 10 до 128 символов");
    else if (/\s/.test(password) || !/[a-zа-яё]/u.test(password) || !/[A-ZА-ЯЁ]/u.test(password) || !/\d/u.test(password) || !/[^\p{L}\p{N}\s]/u.test(password)) {
      mark(passwordInput, "Добавьте строчную и заглавную буквы, цифру и специальный символ");
    }
    if (firstInvalid) {
      firstInvalid.focus();
      return;
    }
    if (entitySelect.value === "edu_institution" && !box.querySelector("#u-partner").value) {
      err.textContent = "Для образовательной организации выберите партнёра";
      err.style.display = "block";
      return;
    }

    const createButton = box.querySelector("#u-create");
    createButton.disabled = true;
    createButton.textContent = "Создаём…";
    try {
      const partnerValue = box.querySelector("#u-partner").value;
      await api("/admin/users", {
        method: "POST",
        body: JSON.stringify({
          email,
          password,
          full_name: fullName,
          role: box.querySelector("#u-role").value,
          entity_type: entitySelect.value,
          partner_id: partnerValue || null,
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

  const users = (await api("/admin/users")) || [];
  const listBox = box.querySelector("#u-list");
  listBox.innerHTML = `<div class="table-wrap"><table><thead><tr><th>Email</th><th>ФИО</th><th>Роль</th><th>Статус</th><th></th></tr></thead>
    <tbody>${users
      .map(
        (u) => `<tr>
      <td>${escapeHTML(u.email)}</td><td>${escapeHTML(u.full_name)}</td><td><span class="role-badge">${escapeHTML(valueLabel(u.role))}</span></td>
      <td><span class="status-badge ${u.is_active ? "active" : "inactive"}">${u.is_active ? "Активен" : "Отключён"}</span></td>
      <td><button class="btn secondary" data-id="${escapeHTML(u.id)}" data-active="${u.is_active}">${u.is_active ? "Отключить" : "Включить"}</button></td>
    </tr>`
      )
      .join("")}</tbody></table></div>`;
  listBox.querySelectorAll("button[data-id]").forEach((b) => {
    b.onclick = async () => {
      b.disabled = true;
      try {
        await api(`/admin/users/${b.dataset.id}`, {
          method: "PATCH",
          body: JSON.stringify({ is_active: b.dataset.active !== "true" }),
        });
        showToast("Статус пользователя обновлён", "success");
        await renderAdminTab(box, "users");
      } catch (e) {
        showToast(e.message);
        b.disabled = false;
      }
    };
  });
}

async function renderAdminPartners(box) {
  box.innerHTML = `<div class="card"><h2>Новый партнёр</h2>
    <form id="p-form" novalidate><div class="grid cols-3">
      <div class="field"><label>Наименование *</label><input id="p-name" maxlength="300" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Вид ОО</label><select id="p-kind"><option value="vuz">Вуз</option><option value="kolledj">Колледж</option><option value="school">Школа</option></select></div>
      <div class="field"><label>№ соглашения</label><input id="p-number" maxlength="100"></div>
    </div>
    <button type="submit" class="btn" id="p-create">Добавить</button><div class="error form-message" id="p-error" style="display:none" role="alert"></div></form>
  </div>
  <div class="card"><h2>Партнёры</h2><div id="p-list">Загрузка…</div></div>`;
  box.querySelector("#p-form").onsubmit = async (event) => {
    event.preventDefault();
    const nameInput = box.querySelector("#p-name");
    const error = box.querySelector("#p-error");
    const button = box.querySelector("#p-create");
    const name = nameInput.value.trim().replace(/\s+/g, " ");
    clearFieldErrors(event.currentTarget);
    error.style.display = "none";
    if (!name) {
      setFieldError(nameInput, "Укажите наименование партнёра");
      nameInput.focus();
      return;
    }
    button.disabled = true;
    button.textContent = "Добавляем…";
    try {
      await api("/partners", {
        method: "POST",
        body: JSON.stringify({
          name,
          partner_kind: box.querySelector("#p-kind").value,
          agreement_number: box.querySelector("#p-number").value.trim(),
        }),
      });
      state.partners = (await api("/partners")) || [];
      showToast("Партнёр добавлен", "success");
      await renderAdminTab(box, "partners");
    } catch (e) {
      error.textContent = e.message;
      error.style.display = "block";
      button.disabled = false;
      button.textContent = "Добавить";
    }
  };
  const partners = (await api("/partners")) || [];
  state.partners = partners;
  box.querySelector("#p-list").innerHTML = `<div class="table-wrap"><table><thead><tr><th>Наименование</th><th>Вид</th><th>№ соглашения</th></tr></thead>
    <tbody>${partners.map((p) => `<tr><td>${escapeHTML(p.name)}</td><td>${escapeHTML(valueLabel(p.partner_kind))}</td><td>${escapeHTML(p.agreement_number || "—")}</td></tr>`).join("")}</tbody></table></div>`;
}

async function renderAdminSettings(box) {
  const settings = await api("/admin/settings");
  box.innerHTML = `<div class="card"><h2>Настройки хранения</h2>
    <div class="grid cols-2">
      <div class="field"><label>Хранение подтверждающих документов, дней</label>
        <input id="s-attach" type="number" min="1" max="3650" step="1" value="${settings.attachment_retention_days || 365}"></div>
      <div class="field"><label>Хранение журнала изменений, дней</label>
        <input id="s-audit" type="number" min="1" max="3650" step="1" value="${settings.audit_log_retention_days || 60}"></div>
    </div>
    <button class="btn" id="s-save">Сохранить</button>
    <p class="muted">По ТЗ подтверждающие документы хранятся год, но администратор может изменить срок; журнал изменений — 2 месяца.</p>
  </div>`;
  box.querySelector("#s-save").onclick = async () => {
    const button = box.querySelector("#s-save");
    const attachmentDays = Number(box.querySelector("#s-attach").value);
    const auditDays = Number(box.querySelector("#s-audit").value);
    if (![attachmentDays, auditDays].every((value) => Number.isInteger(value) && value >= 1 && value <= 3650)) {
      showToast("Срок хранения должен быть целым числом от 1 до 3650 дней");
      return;
    }
    button.disabled = true;
    button.textContent = "Сохраняем…";
    try {
      await api("/admin/settings", { method: "POST", body: JSON.stringify({ key: "attachment_retention_days", value: String(attachmentDays) }) });
      await api("/admin/settings", { method: "POST", body: JSON.stringify({ key: "audit_log_retention_days", value: String(auditDays) }) });
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
  box.innerHTML = `<div class="card"><h2>Журнал изменений (хранится согласно настройке выше)</h2><div id="logs-list">Загрузка…</div></div>`;
  const logs = (await api("/admin/logs?limit=200")) || [];
  box.querySelector("#logs-list").innerHTML = `<div class="table-wrap"><table><thead><tr><th>Дата</th><th>Объект</th><th>Действие</th><th>Комментарий</th></tr></thead>
    <tbody>${logs
      .map(
        (l) => `<tr>
      <td>${new Date(l.created_at).toLocaleString("ru-RU")}</td>
      <td>${escapeHTML(valueLabel(l.entity_type))}${l.entity_id ? " #" + escapeHTML(l.entity_id.slice(0, 8)) : ""}</td>
      <td>${escapeHTML(valueLabel(l.action))}</td>
      <td>${escapeHTML(l.comment_text || "")}</td>
    </tr>`
      )
      .join("")}</tbody></table></div>`;
}

boot();
