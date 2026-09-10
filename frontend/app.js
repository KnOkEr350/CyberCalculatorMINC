// Калькулятор затрат по Приказу Минцифры — фронтенд.
// Без фреймворков и сборки: чистый fetch + DOM. Один файл, простой роутер по хэшу.

const state = {
  me: null,
  categories: [],
  partners: [],
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
  admin: "Администратор",
  organization: "Киберпротект",
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
  if (Array.isArray(data) && data.nextOffset != null && path.split("?")[0] !== "/entries") {
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
      (state.me.entity_type === "edu_institution" && !state.me.partner_id))
  ) {
    app.appendChild(
      el(
        `<div class="card"><h2>Профиль не назначен</h2><p>Попросите администратора назначить роль и учебное заведение. Самостоятельная смена прав недоступна.</p><button class="btn" id="reload-profile">Проверить назначение</button><button class="btn secondary" id="unassigned-logout">Выйти</button></div>`,
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
        <div class="field"><label for="login-code">Код двухфакторной защиты, если настроена</label><input id="login-code" autocomplete="one-time-code" maxlength="20" placeholder="6 цифр или резервный код"></div>
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
  const wrap = el(`<div>
    <div class="topbar">
      ${brandMarkup()}
      <nav>
        <button data-view="dashboard">Дашборд</button>
        <button data-view="entries">План / Факт</button>
        <button data-view="partners">Учебные заведения</button>
        ${isAdmin ? '<button data-view="admin">Админка</button>' : ""}
      </nav>
      <div class="who">
        <span class="avatar">${escapeHTML(initials(state.me.full_name))}</span>
        <span class="user-copy"><strong>${escapeHTML(state.me.full_name)}</strong><small>${isStaffUser() ? "Киберпротект" : "Учебное заведение"}${isAdmin ? " · Администратор" : ""}</small></span>
        <button id="change-password" title="Изменить пароль">Пароль</button>
        ${state.me.mfa_available && !state.me.mfa_enabled ? '<button id="setup-mfa">Защита входа</button>' : ''}
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
  else if (state.view === "admin") renderAdmin(content);
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

  root.innerHTML = `
    <section class="page-heading">
      <div><span class="eyebrow">Аналитика</span><h1>Дашборд</h1><p>Суммы внесённых мероприятий за выбранный год. Проверка обязательных активностей — в плане/факте партнёра; суммы сами по себе не подтверждают соответствие приказу.</p></div>
      <span class="year-badge">${state.year}</span>
    </section>
    <div class="card">
      <div class="flex between">
        <h2 style="margin:0">Пульс проекта</h2>
        <div class="flex">
          <label style="margin:0">Год</label>
          <input type="number" id="dash-year" min="2000" max="2100" step="1" value="${state.year}" style="width:90px">
          <label for="dash-partner">Партнёр</label><select id="dash-partner"><option value="">Все доступные партнёры</option>${state.partners.map((p) => `<option value="${p.id}" ${p.id === state.partnerID ? "selected" : ""}>${escapeHTML(p.name)}</option>`).join("")}</select>
          ${
            state.me.role === "admin"
              ? `<button class="btn secondary" id="set-target">Задать целевую сумму (3%)</button>`
              : ""
          }
        </div>
      </div>
      <div class="grid cols-3" style="margin-top:14px">
        <div class="stat"><div class="label">Моя целевая сумма (3% от льгот)</div><div class="value">${
          d.target_amount_rub != null
            ? fmtMoney(d.target_amount_rub)
            : "не задана"
        }</div></div>
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
      <div class="card"><h2>Утверждённые суммы</h2><p>План: ${fmtMoney(d.eligible_plan_total_rub)}. Факт: ${fmtMoney(d.eligible_fact_total_rub)}.</p><p>Записей в неутверждённых отчётах: ${Number(d.incomplete_entries || 0)}. Отдельные преподавание или ООП/РПД больше не засчитываются сами по себе: учитывается только полностью проверенный и утверждённый комплект по соглашению. Исключение ТОП ИТ/ИИ применяется лишь при наличии утверждённых остальных видов в другой ОО.</p></div>
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
  root.querySelector("#dash-partner").onchange = (e) => {
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
      root
        .querySelectorAll(".tabs button")
        .forEach((x) => x.classList.remove("active"));
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
    if (tab === "partners") return await renderPartnerDirectory(box);
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
      <div class="field"><label>Email *</label><input id="u-email" type="email" maxlength="254" autocomplete="off" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Пароль *</label><input id="u-password" type="password" minlength="10" maxlength="128" autocomplete="new-password" required><div class="field-hint">10–128 символов: A–Z, a–z, цифра и спецсимвол</div><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>ФИО *</label><input id="u-name" minlength="2" maxlength="200" required><div class="field-error" style="display:none"></div></div>
      <div class="field"><label>Роль</label><select id="u-role"><option value="user">Пользователь</option><option value="admin">Администратор</option></select></div>
      <div class="field"><label>Тип пользователя</label><select id="u-entity"><option value="organization">Сотрудник Киберпротекта</option><option value="edu_institution">Представитель учебного заведения</option></select></div>
      <div class="field" id="u-partner-field" style="display:none"><label>Партнёр</label><select id="u-partner"><option value="">Не назначен</option>${(Array.isArray(
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
    </div>
    <button type="submit" class="btn" id="u-create">Создать</button>
    <div class="error" id="u-error" style="display:none"></div>
    </form>
  </div>
  <div class="card"><h2>Пользователи</h2><div id="u-list">Загрузка…</div></div>`;

  const entitySelect = box.querySelector("#u-entity");
  entitySelect.onchange = () => {
    box.querySelector("#u-partner-field").style.display =
      entitySelect.value === "edu_institution" ? "block" : "none";
    if (entitySelect.value !== "edu_institution")
      box.querySelector("#u-partner").value = "";
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
    if (firstInvalid) {
      firstInvalid.focus();
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
      <td><button class="btn secondary" data-id="${escapeHTML(u.id)}" data-active="${u.is_active}">${u.is_active ? "Отключить" : "Включить"}</button><button class="btn secondary" data-profile="${escapeHTML(u.id)}">Профиль и доступ</button></td>
    </tr>`,
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
  listBox.querySelectorAll("[data-profile]").forEach(
    (b) =>
      (b.onclick = () =>
        openUserProfile(
          users.find((u) => u.id === b.dataset.profile),
          () => renderAdminUsers(box),
        )),
  );
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
    <p class="muted">По ТЗ подтверждающие документы хранятся год, но администратор может изменить срок; журнал изменений — 2 месяца.</p>
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
  box.innerHTML = `<div class="card"><h2>Журнал изменений (хранится согласно настройке выше)</h2><div id="logs-list">Загрузка…</div></div>`;
  const logs = (await api("/admin/logs?limit=200")) || [];
  box.querySelector("#logs-list").innerHTML =
    `<div class="table-wrap"><table><thead><tr><th>Дата</th><th>Объект</th><th>Действие</th><th>Комментарий</th></tr></thead>
    <tbody>${logs
      .map(
        (l) => `<tr>
      <td>${new Date(l.created_at).toLocaleString("ru-RU")}</td>
      <td>${escapeHTML(valueLabel(l.entity_type))}${l.entity_id ? " #" + escapeHTML(l.entity_id.slice(0, 8)) : ""}</td>
      <td>${escapeHTML(valueLabel(l.action))}</td>
      <td>${escapeHTML(l.comment_text || "")}</td>
    </tr>`,
      )
      .join("")}</tbody></table></div>`;
}

boot();
