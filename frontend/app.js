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
};

const CATEGORY_LABELS = {}; // заполняется из /api/categories

async function api(path, opts = {}) {
  const res = await fetch("/api" + path, {
    credentials: "same-origin",
    headers: opts.body && !(opts.body instanceof FormData) ? { "Content-Type": "application/json" } : undefined,
    ...opts,
  });
  if (res.status === 401) {
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

// ---------------------------------------------------------------- ROUTER --

async function boot() {
  try {
    state.me = await api("/auth/me");
  } catch (e) {
    state.me = null;
  }
  if (state.me) {
    try {
      state.categories = await api("/categories");
      state.categories.forEach((c) => (CATEGORY_LABELS[c.code] = c.name));
      state.partners = await api("/partners");
    } catch (e) {}
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
  const wrap = el(`<div class="login-wrap"><div class="login-box card">
    <h1>Калькулятор затрат по Приказу Минцифры</h1>
    <div class="field"><label>Email</label><input type="email" id="login-email" style="width:100%"></div>
    <div class="field"><label>Пароль</label><input type="password" id="login-password" style="width:100%"></div>
    <div class="error" id="login-error" style="display:none"></div>
    <button class="btn" id="login-submit" style="width:100%">Войти</button>
  </div></div>`);
  wrap.querySelector("#login-submit").onclick = async () => {
    const email = wrap.querySelector("#login-email").value.trim();
    const password = wrap.querySelector("#login-password").value;
    const errBox = wrap.querySelector("#login-error");
    try {
      await api("/auth/login", { method: "POST", body: JSON.stringify({ email, password }) });
      await boot();
    } catch (e) {
      errBox.textContent = e.message;
      errBox.style.display = "block";
    }
  };
  return wrap;
}

function renderChooseEntity() {
  const wrap = el(`<div class="login-wrap"><div class="login-box card">
    <h1>Кто вы?</h1>
    <p class="muted">Выберите роль — от этого зависит, как калькулятор считает и группирует ваши отчёты.</p>
    <div class="field">
      <button class="btn" id="pick-org" style="width:100%;margin-bottom:8px">Организация (ИТ-компания)</button>
      <button class="btn secondary" id="pick-edu" style="width:100%">Вуз / СПО (партнёр)</button>
    </div>
  </div></div>`);
  const pick = async (entity_type) => {
    await api("/auth/entity-type", { method: "POST", body: JSON.stringify({ entity_type }) });
    await boot();
  };
  wrap.querySelector("#pick-org").onclick = () => pick("organization");
  wrap.querySelector("#pick-edu").onclick = () => pick("edu_institution");
  return wrap;
}

// ---------------------------------------------------------------- LAYOUT --

function renderLayout() {
  const isAdmin = state.me.role === "admin";
  const wrap = el(`<div>
    <div class="topbar">
      <h1>Калькулятор затрат — Приказ Минцифры</h1>
      <nav>
        <button data-view="dashboard">Дашборд</button>
        <button data-view="entries">План / Факт</button>
        ${isAdmin ? '<button data-view="admin">Админка</button>' : ""}
      </nav>
      <div class="who">
        <span>${state.me.full_name} · ${state.me.entity_type === "organization" ? "Организация" : "Вуз/СПО"}${isAdmin ? " · admin" : ""}</span>
        <button id="logout">Выйти</button>
      </div>
    </div>
    <div class="container" id="content"></div>
  </div>`);
  wrap.querySelectorAll("nav button").forEach((b) => {
    if (b.dataset.view === state.view) b.classList.add("active");
    b.onclick = () => {
      state.view = b.dataset.view;
      render();
    };
  });
  wrap.querySelector("#logout").onclick = async () => {
    await api("/auth/logout", { method: "POST" });
    state.me = null;
    render();
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
    root.innerHTML = `<div class="error">${e.message}</div>`;
    return;
  }
  state.dashboard = d;

  const pctFor = (arr) =>
    arr && arr.length
      ? arr
          .map(
            (b) => `<div class="bar-row">
        <div class="name">${CATEGORY_LABELS[b.category_code] || b.category_code}</div>
        <div class="bar-track"><div class="bar-fill" style="width:${b.share_percent}%"></div></div>
        <div class="pct">${fmtMoney(b.amount_rub)}</div>
      </div>`
          )
          .join("")
      : `<div class="muted">Нет данных за ${state.year} год</div>`;

  root.innerHTML = `
    <div class="card">
      <div class="flex between">
        <h2 style="margin:0">Пульс проекта</h2>
        <div class="flex">
          <label style="margin:0">Год</label>
          <input type="number" id="dash-year" value="${state.year}" style="width:90px">
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
      <div class="grid cols-2" style="margin-top:10px">
        <div class="stat"><div class="label">% реализации плана (факт/план)</div><div class="value">${d.plan_completion_pct}%</div></div>
      </div>
    </div>
    <div class="grid cols-2">
      <div class="card"><h2>Структура — План</h2>${pctFor(d.plan_by_category)}</div>
      <div class="card"><h2>Структура — Факт</h2>${pctFor(d.fact_by_category)}</div>
    </div>
  `;
  root.querySelector("#dash-year").onchange = (e) => {
    state.year = parseInt(e.target.value, 10) || state.year;
    render();
  };
  const targetBtn = root.querySelector("#set-target");
  if (targetBtn) {
    targetBtn.onclick = async () => {
      const val = prompt("Целевая сумма затрат на " + state.year + " год, руб. (3% от сэкономленных льгот):", d.target_amount_rub || "");
      if (val == null) return;
      await api("/dashboard/target", {
        method: "POST",
        body: JSON.stringify({ report_year: state.year, target_amount_rub: parseFloat(val) }),
      });
      render();
    };
  }
}

// --------------------------------------------------------------- ENTRIES --

async function renderEntries(root) {
  if (!state.categoryCode && state.categories.length) state.categoryCode = state.categories[0].code;

  root.innerHTML = `<div class="card">
    <div class="tabs">
      <button data-p="plan" class="${state.period === "plan" ? "active" : ""}">План</button>
      <button data-p="fact" class="${state.period === "fact" ? "active" : ""}">Факт</button>
    </div>
    <div class="grid cols-3">
      <div class="field"><label>Год</label><input type="number" id="year" value="${state.year}"></div>
      <div class="field"><label>Категория активности</label>
        <select id="category">${state.categories
          .map((c) => `<option value="${c.code}" ${c.code === state.categoryCode ? "selected" : ""}>${c.name}</option>`)
          .join("")}</select>
      </div>
      <div class="field right" style="align-self:end">
        <button class="btn" id="add-entry">+ Добавить запись</button>
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
    state.year = parseInt(e.target.value, 10) || state.year;
    renderEntries(root);
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
    box.innerHTML = `<div class="error">${e.message}</div>`;
    return;
  }
  if (!state.entries.length) {
    box.innerHTML = `<div class="muted">Записей пока нет</div>`;
    return;
  }
  const total = state.entries.reduce((s, e) => s + Number(e.amount_rub), 0);
  box.innerHTML = `<table>
    <thead><tr><th>Партнёр</th><th>Аудитория</th><th>Сумма, руб.</th><th></th></tr></thead>
    <tbody>
      ${state.entries
        .map(
          (e) => `<tr data-id="${e.id}">
        <td>${partnerName(e.partner_id)}</td>
        <td>${e.audience}</td>
        <td>${fmtMoney(e.amount_rub)}</td>
        <td class="muted">ред.</td>
      </tr>`
        )
        .join("")}
    </tbody>
    <tfoot><tr><td colspan="2"><b>Итого</b></td><td><b>${fmtMoney(total)}</b></td><td></td></tr></tfoot>
  </table>`;
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

function fieldInput(f, value) {
  const val = value === undefined || value === null ? "" : value;
  if (f.type === "select") {
    const opts = f.key === "org_name" ? state.partners.map((p) => ({ v: p.id, l: p.name })) : (f.options || []).map((o) => ({ v: o, l: o }));
    return `<select data-key="${f.key}" data-kind="select">
      <option value="">—</option>
      ${opts.map((o) => `<option value="${o.v}" ${o.v === val ? "selected" : ""}>${o.l}</option>`).join("")}
    </select>`;
  }
  if (f.type === "number") {
    return `<input type="number" min="0" step="any" data-key="${f.key}" data-kind="number" value="${val}">`;
  }
  return `<input type="text" data-key="${f.key}" data-kind="text" value="${val}">`;
}

async function openEntryModal(entry) {
  const cat = currentCategory();
  if (!cat) return;
  const isEdit = !!entry;
  const payload = entry ? entry.payload || {} : {};
  const audience = entry ? entry.audience : cat.audience_scope[0];

  const backdrop = el(`<div class="modal-backdrop"><div class="modal">
    <h2 style="margin-top:0">${isEdit ? "Редактировать запись" : "Новая запись"} — ${cat.name}</h2>
    <div class="field"><label>Аудитория</label>
      <select id="m-audience">${cat.audience_scope.map((a) => `<option value="${a}" ${a === audience ? "selected" : ""}>${a}</option>`).join("")}</select>
    </div>
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
        <button class="btn secondary" id="m-cancel">Отмена</button>
        <button class="btn" id="m-save">${isEdit ? "Сохранить" : "Создать"}</button>
      </div>
    </div>
    ${isEdit && entry.period_type === "fact" ? renderAttachSection() : ""}
  </div></div>`);

  const fieldsBox = backdrop.querySelector("#m-fields");
  cat.fields.forEach((f) => {
    const row = el(`<div class="field"><label>${f.label}${f.required ? " *" : ""}</label></div>`);
    row.appendChild(el(fieldInput(f, payload[f.key])));
    fieldsBox.appendChild(row);
  });

  backdrop.querySelector("#m-cancel").onclick = () => backdrop.remove();
  backdrop.querySelector("#m-save").onclick = async () => {
    const newPayload = {};
    fieldsBox.querySelectorAll("[data-key]").forEach((inp) => {
      const v = inp.value;
      if (v === "") return;
      newPayload[inp.dataset.key] = inp.dataset.kind === "number" ? parseFloat(v) : v;
    });
    const aud = backdrop.querySelector("#m-audience").value;
    const errBox = backdrop.querySelector("#m-error");
    try {
      if (isEdit) {
        const comment = backdrop.querySelector("#m-comment").value.trim();
        if (!comment) throw new Error("Комментарий обязателен при редактировании");
        await api(`/entries/${entry.id}`, {
          method: "PUT",
          body: JSON.stringify({ payload: newPayload, audience: aud, comment }),
        });
      } else {
        const partnerId = newPayload.org_name || null;
        await api(`/entries`, {
          method: "POST",
          body: JSON.stringify({
            category_code: state.categoryCode,
            partner_id: partnerId,
            period_type: state.period,
            report_year: state.year,
            audience: aud,
            payload: newPayload,
          }),
        });
      }
      backdrop.remove();
      const root = document.getElementById("content");
      await loadEntriesTable(root);
    } catch (e) {
      errBox.textContent = e.message;
      errBox.style.display = "block";
    }
  };

  document.body.appendChild(backdrop);

  if (isEdit && entry.period_type === "fact") {
    wireAttachSection(backdrop, entry.id);
  }
}

function renderAttachSection() {
  return `<div class="card" style="margin-top:14px;background:transparent;padding:0;border:none">
    <h2>Подтверждающий документ</h2>
    <div id="attach-list" class="attach-list muted">Загрузка…</div>
    <div class="field" style="margin-top:8px">
      <input type="file" id="attach-file">
      <button class="btn secondary" id="attach-upload">Загрузить</button>
    </div>
  </div>`;
}

async function wireAttachSection(root, entryId) {
  const list = root.querySelector("#attach-list");
  const refresh = async () => {
    try {
      const items = await api(`/entries/${entryId}/attachments`);
      list.innerHTML = items.length
        ? items
            .map(
              (a) =>
                `<div>📄 <a href="/api/attachments/${a.id}/download">${a.file_name}</a> <span class="muted">(до ${new Date(
                  a.retention_expires_at
                ).toLocaleDateString("ru-RU")})</span></div>`
            )
            .join("")
        : `<span class="muted">Файлов пока нет</span>`;
    } catch (e) {
      list.innerHTML = `<span class="error">${e.message}</span>`;
    }
  };
  root.querySelector("#attach-upload").onclick = async () => {
    const fileInput = root.querySelector("#attach-file");
    if (!fileInput.files.length) return;
    const fd = new FormData();
    fd.append("file", fileInput.files[0]);
    await fetch(`/api/entries/${entryId}/attachments`, { method: "POST", body: fd, credentials: "same-origin" });
    fileInput.value = "";
    refresh();
  };
  refresh();
}

// ----------------------------------------------------------------- ADMIN --

async function renderAdmin(root) {
  root.innerHTML = `<div class="tabs">
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
  if (tab === "users") return renderAdminUsers(box);
  if (tab === "partners") return renderAdminPartners(box);
  if (tab === "settings") return renderAdminSettings(box);
  if (tab === "logs") return renderAdminLogs(box);
}

async function renderAdminUsers(box) {
  box.innerHTML = `<div class="card"><h2>Новый пользователь (в т.ч. дополнительный админ)</h2>
    <div class="grid cols-3">
      <div class="field"><label>Email</label><input id="u-email"></div>
      <div class="field"><label>Пароль</label><input id="u-password" type="password"></div>
      <div class="field"><label>ФИО</label><input id="u-name"></div>
      <div class="field"><label>Роль</label><select id="u-role"><option value="user">user</option><option value="admin">admin</option></select></div>
    </div>
    <button class="btn" id="u-create">Создать</button>
    <div class="error" id="u-error" style="display:none"></div>
  </div>
  <div class="card"><h2>Пользователи</h2><div id="u-list">Загрузка…</div></div>`;

  box.querySelector("#u-create").onclick = async () => {
    const err = box.querySelector("#u-error");
    try {
      await api("/admin/users", {
        method: "POST",
        body: JSON.stringify({
          email: box.querySelector("#u-email").value.trim(),
          password: box.querySelector("#u-password").value,
          full_name: box.querySelector("#u-name").value.trim(),
          role: box.querySelector("#u-role").value,
        }),
      });
      renderAdminUsers(box);
    } catch (e) {
      err.textContent = e.message;
      err.style.display = "block";
    }
  };

  const users = await api("/admin/users");
  const listBox = box.querySelector("#u-list");
  listBox.innerHTML = `<table><thead><tr><th>Email</th><th>ФИО</th><th>Роль</th><th>Статус</th><th></th></tr></thead>
    <tbody>${users
      .map(
        (u) => `<tr>
      <td>${u.email}</td><td>${u.full_name}</td><td>${u.role}</td>
      <td>${u.is_active ? "активен" : "отключён"}</td>
      <td><button class="btn secondary" data-id="${u.id}" data-active="${u.is_active}">${u.is_active ? "Отключить" : "Включить"}</button></td>
    </tr>`
      )
      .join("")}</tbody></table>`;
  listBox.querySelectorAll("button[data-id]").forEach((b) => {
    b.onclick = async () => {
      await api(`/admin/users/${b.dataset.id}`, {
        method: "PATCH",
        body: JSON.stringify({ is_active: b.dataset.active !== "true" }),
      });
      renderAdminUsers(box);
    };
  });
}

async function renderAdminPartners(box) {
  box.innerHTML = `<div class="card"><h2>Новый партнёр</h2>
    <div class="grid cols-3">
      <div class="field"><label>Наименование</label><input id="p-name"></div>
      <div class="field"><label>Вид ОО</label><select id="p-kind"><option value="vuz">Вуз</option><option value="kolledj">Колледж</option><option value="school">Школа</option></select></div>
      <div class="field"><label>№ соглашения</label><input id="p-number"></div>
    </div>
    <button class="btn" id="p-create">Добавить</button>
  </div>
  <div class="card"><h2>Партнёры</h2><div id="p-list">Загрузка…</div></div>`;
  box.querySelector("#p-create").onclick = async () => {
    await api("/partners", {
      method: "POST",
      body: JSON.stringify({
        name: box.querySelector("#p-name").value.trim(),
        partner_kind: box.querySelector("#p-kind").value,
        agreement_number: box.querySelector("#p-number").value.trim(),
      }),
    });
    state.partners = await api("/partners");
    renderAdminPartners(box);
  };
  const partners = await api("/partners");
  state.partners = partners;
  box.querySelector("#p-list").innerHTML = `<table><thead><tr><th>Наименование</th><th>Вид</th><th>№ соглашения</th></tr></thead>
    <tbody>${partners.map((p) => `<tr><td>${p.name}</td><td>${p.partner_kind}</td><td>${p.agreement_number || "—"}</td></tr>`).join("")}</tbody></table>`;
}

async function renderAdminSettings(box) {
  const settings = await api("/admin/settings");
  box.innerHTML = `<div class="card"><h2>Настройки хранения</h2>
    <div class="grid cols-2">
      <div class="field"><label>Хранение подтверждающих документов, дней</label>
        <input id="s-attach" type="number" value="${settings.attachment_retention_days || 365}"></div>
      <div class="field"><label>Хранение журнала изменений, дней</label>
        <input id="s-audit" type="number" value="${settings.audit_log_retention_days || 60}"></div>
    </div>
    <button class="btn" id="s-save">Сохранить</button>
    <p class="muted">По ТЗ подтверждающие документы хранятся год, но администратор может изменить срок; журнал изменений — 2 месяца.</p>
  </div>`;
  box.querySelector("#s-save").onclick = async () => {
    await api("/admin/settings", { method: "POST", body: JSON.stringify({ key: "attachment_retention_days", value: box.querySelector("#s-attach").value }) });
    await api("/admin/settings", { method: "POST", body: JSON.stringify({ key: "audit_log_retention_days", value: box.querySelector("#s-audit").value }) });
    renderAdminSettings(box);
  };
}

async function renderAdminLogs(box) {
  box.innerHTML = `<div class="card"><h2>Журнал изменений (хранится согласно настройке выше)</h2><div id="logs-list">Загрузка…</div></div>`;
  const logs = await api("/admin/logs?limit=200");
  box.querySelector("#logs-list").innerHTML = `<table><thead><tr><th>Дата</th><th>Объект</th><th>Действие</th><th>Комментарий</th></tr></thead>
    <tbody>${logs
      .map(
        (l) => `<tr>
      <td>${new Date(l.created_at).toLocaleString("ru-RU")}</td>
      <td>${l.entity_type}${l.entity_id ? " #" + l.entity_id.slice(0, 8) : ""}</td>
      <td>${l.action}</td>
      <td>${l.comment_text || ""}</td>
    </tr>`
      )
      .join("")}</tbody></table>`;
}

boot();
