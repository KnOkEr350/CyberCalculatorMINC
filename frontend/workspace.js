// Partner-first workflows. Formula fields come from the backend registry.
function isStaffUser() {
  return state.me?.role === "admin" || state.me?.entity_type === "organization";
}

function openUserProfile(user, refresh) {
  const modal = el(
    `<div class="modal-backdrop"><form class="modal" role="dialog" aria-modal="true"><h2>Доступ: ${escapeHTML(user.full_name)}</h2><div class="field"><label>Роль</label><select name="role"><option value="user">Пользователь</option><option value="admin">Администратор — полный доступ</option></select></div><div class="field"><label>Представляет</label><select name="entity"><option value="organization">Киберпротект — все партнёры</option><option value="edu_institution">Учебное заведение — только свой партнёр</option></select></div><div class="field"><label>Учебное заведение</label><select name="partner"><option value="">Не назначено</option>${state.partners.map((p) => `<option value="${p.id}">${escapeHTML(p.name)}</option>`).join("")}</select></div><p class="error" role="alert"></p><button class="btn" type="submit">Сохранить доступ</button><button class="btn secondary" type="button">Закрыть</button></form></div>`,
  );
  const form = modal.querySelector("form");
  form.elements.role.value = user.role;
  form.elements.entity.value = user.entity_type || "edu_institution";
  form.elements.partner.value = user.partner_id || "";
  const sync = () => {
    if (form.elements.role.value === "admin")
      form.elements.entity.value = "organization";
    form.elements.entity.disabled = form.elements.role.value === "admin";
    form.elements.partner.disabled =
      form.elements.entity.value !== "edu_institution";
  };
  form.elements.role.onchange = sync;
  form.elements.entity.onchange = sync;
  sync();
  form.querySelector("[type=button]").onclick = () => modal.remove();
  form.onsubmit = async (e) => {
    e.preventDefault();
    const b = form.querySelector("[type=submit]");
    b.disabled = true;
    try {
      await api(`/admin/users/${user.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          role: form.elements.role.value,
          entity_type: form.elements.entity.value,
          partner_id: form.elements.partner.disabled
            ? ""
            : form.elements.partner.value,
        }),
      });
      modal.remove();
      await refresh();
      if (user.id === state.me.id) await boot();
    } catch (err) {
      form.querySelector(".error").textContent = err.message;
    } finally {
      b.disabled = false;
    }
  };
  document.body.appendChild(modal);
}
function validateFileBatch(files) {
  if (
    files.length > 20 ||
    [...files].reduce((sum, f) => sum + f.size, 0) > 64 * 1024 * 1024
  )
    throw new Error("Выберите до 20 файлов общим размером до 64 МБ");
  if ([...files].some((f) => !f.size || [...f.name].length > 255))
    throw new Error("Пустые файлы и имена длиннее 255 символов не допускаются");
}
async function uploadFileBatch(id, files) {
  validateFileBatch(files);
  if (!files.length) return;
  const body = new FormData();
  [...files].forEach((f) => body.append("files", f));
  await api(`/entries/${encodeURIComponent(id)}/attachments`, {
    method: "POST",
    body,
  });
}
function workspaceQuery() {
  return new URLSearchParams({
    partner_id: state.partnerID,
    category_code: state.categoryCode,
    period_type: state.period,
    report_year: state.year,
  });
}
async function renderPartnerEntries(root) {
  const generation = (root.workspaceGeneration || 0) + 1;
  root.workspaceGeneration = generation;
  if (!state.categories.length) {
    state.categories = await api("/categories");
    state.categories.forEach((c) => (CATEGORY_LABELS[c.code] = c.name));
  }
  let partner = state.partners.find((p) => p.id === state.partnerID);
  if (!partner && state.partners.length === 1) {
    partner = state.partners[0];
    state.partnerID = partner.id;
  }
  if (partner) state.partnerKind = partner.partner_kind;
  const available = state.categories.filter((c) =>
    c.audience_scope.includes(partner?.partner_kind || state.partnerKind),
  );
  if (!available.some((c) => c.code === state.categoryCode))
    state.categoryCode = available[0]?.code || "";
  root.innerHTML = `<section class="page-heading"><div><span class="eyebrow">Работа с партнёром</span><h1>План и факт</h1><p>Учебное заведение → доступная активность → расчёт и необязательные вложения.</p></div></section>
  <div class="card"><div class="grid cols-3">
    <div class="field"><label for="workspace-kind">1. Тип ОО</label><select id="workspace-kind">${Object.entries(
      AUDIENCE_LABELS,
    )
      .map(
        ([k, v]) =>
          `<option value="${k}" ${k === state.partnerKind ? "selected" : ""}>${v}</option>`,
      )
      .join("")}</select></div>
    <div class="field"><label for="partner-search">Поиск своего партнёра</label><input id="partner-search" placeholder="Часть названия"></div>
    <div class="field"><label for="workspace-partner">2. Учебное заведение</label><select id="workspace-partner"></select></div>
  </div><button class="btn secondary" id="open-directory">Справочник учебных заведений</button><p class="muted">${partner ? `Соглашение: ${escapeHTML(partner.agreement_number || "не указано")}; дата: ${escapeHTML(partner.agreement_date || "не указана")}` : "Сначала выберите партнёра. Нового партнёра добавляет сотрудник Киберпротекта."}</p></div>
  <div class="card"><div class="tabs"><button data-p="plan" class="${state.period === "plan" ? "active" : ""}">План</button><button data-p="fact" class="${state.period === "fact" ? "active" : ""}">Факт</button></div>
    <div class="grid cols-3"><div class="field"><label>Год</label><input type="number" id="year" min="2000" max="2100" step="1" value="${state.year}"></div>
    <div class="field"><label>3. Категория активности</label><select id="category">${available.map((c) => `<option value="${c.code}" ${c.code === state.categoryCode ? "selected" : ""}>${escapeHTML(c.name)}</option>`).join("")}</select></div>
    <div class="field"><label>Действия</label><button class="btn" id="add-entry" ${partner ? "" : "disabled"}>+ Добавить запись</button></div></div>
    <div class="flex"><button class="btn secondary" id="import-entries" ${partner ? "" : "disabled"}>Импорт из Excel</button><a class="btn secondary" id="export-link">Excel: категория</a><a class="btn secondary" id="export-all-link">Excel: все активности партнёра</a><a class="btn secondary" id="export-word">Word: таблица</a></div>
  </div><div id="obligation-box"></div>
  <div class="card"><div class="field"><label for="entry-search">Поиск по реквизитам, студенту, наставнику, программе</label><input id="entry-search" placeholder="Введите текст"></div><div id="entries-table">${partner ? "Загрузка…" : "Выберите учебное заведение выше"}</div></div>`;
  const partnerSelect = root.querySelector("#workspace-partner");
  const fillPartners = () => {
    const term = root
      .querySelector("#partner-search")
      .value.trim()
      .toLocaleLowerCase("ru");
    partnerSelect.innerHTML =
      '<option value="">— Выберите —</option>' +
      state.partners
        .filter(
          (p) =>
            p.partner_kind === state.partnerKind &&
            (p.id === state.partnerID ||
              p.name.toLocaleLowerCase("ru").includes(term)),
        )
        .map(
          (p) =>
            `<option value="${p.id}" ${p.id === state.partnerID ? "selected" : ""}>${escapeHTML(p.name)}</option>`,
        )
        .join("");
  };
  fillPartners();
  root.querySelector("#partner-search").oninput = fillPartners;
  root.querySelector("#workspace-kind").onchange = (e) => {
    state.partnerKind = e.target.value;
    state.partnerID = "";
    renderEntries(root);
  };
  partnerSelect.onchange = (e) => {
    state.partnerID = e.target.value;
    renderEntries(root);
  };
  root.querySelector("#open-directory").onclick = () => {
    state.view = "partners";
    render();
  };
  root.querySelectorAll("[data-p]").forEach(
    (b) =>
      (b.onclick = () => {
        state.period = b.dataset.p;
        renderEntries(root);
      }),
  );
  root.querySelector("#year").onchange = (e) => {
    const year = Number(e.target.value);
    if (!validYear(year)) {
      e.target.reportValidity();
      return;
    }
    state.year = year;
    renderEntries(root);
  };
  root.querySelector("#category").onchange = (e) => {
    state.categoryCode = e.target.value;
    renderEntries(root);
  };
  root.querySelector("#add-entry").onclick = () => openEntryModal(null);
  root.querySelector("#import-entries").onclick = () => openImportDialog(false);
  const query = workspaceQuery();
  const pageKey = String(query);
  if (state.entryPageKey !== pageKey) { state.entryPageKey = pageKey; state.entryPageOffset = 0; }
  root.querySelector("#export-link").href = `/api/reports/export?${query}`;
  const all = new URLSearchParams(query);
  all.delete("category_code");
  root.querySelector("#export-all-link").href = `/api/reports/export?${all}`;
  root.querySelector("#export-word").href =
    `/api/reports/export?${query}&format=docx`;
  if (!partner) {
    root.querySelectorAll("a").forEach((a) => a.removeAttribute("href"));
    return;
  }
  const chosen = state.partnerID;
  const [entries, status] = await Promise.all([
    api(`/entries?${query}&offset=${state.entryPageOffset || 0}`),
    api(`/obligations?${query}`),
  ]);
  if (
    chosen !== state.partnerID ||
    root.workspaceGeneration !== generation ||
    !root.querySelector("#entries-table")
  )
    return;
  state.entries = entries;
  const obligation = root.querySelector("#obligation-box");
  if (partner.partner_kind === "vuz") {
    const topSelected = state.categoryCode === "top_it";
    obligation.innerHTML = `<div class="card"><h2>${status.top_it ? "ТОП ИТ: обязательность снята" : topSelected ? "ТОП ИТ: обязательность будет снята после сохранения" : "Обязательные активности вуза"}</h2>
    <p>${status.top_it || topSelected ? "Преподавание и ООП/РПД не требуются для этого партнёра в выбранных году и плане/факте." : "Для выбранного года проверяются преподавание и хотя бы одна активность ООП/РПД. Записи можно сохранять поэтапно."}</p>
    ${!status.top_it && !topSelected ? ["teachers", "ood_rpd"].map((code) => `<button class="btn secondary" data-required="${code}">${status[code] ? "✓" : "＋"} ${escapeHTML(CATEGORY_LABELS[code])}</button>`).join("") : ""}
    <p class="muted">Это контроль заполнения, не заключение о соответствии приказу. Для льготы ТОП ИТ приказ дополнительно требует другие виды мероприятий хотя бы в одной иной образовательной организации; это условие нужно проверить отдельно.</p></div>`;
    obligation.querySelectorAll("[data-required]").forEach(
      (b) =>
        (b.onclick = () => {
          state.categoryCode = b.dataset.required;
          renderEntries(root);
        }),
    );
  }
  const paint = () => {
    const term = root
      .querySelector("#entry-search")
      .value.toLocaleLowerCase("ru");
    const list = entries.filter((e) =>
      Object.values(e.payload || {})
        .join(" ")
        .toLocaleLowerCase("ru")
        .includes(term),
    );
    const fields =
      currentCategory()?.fields.filter(
        (f) => !["org_name", "mentor_id"].includes(f.key),
      ) || [];
    root.querySelector("#entries-table").innerHTML =
      `<p>Записей: ${list.length} · Сумма: <b>${fmtMoney(list.reduce((s, e) => s + Number(e.amount_rub), 0))}</b></p><div class="table-wrap"><table><thead><tr>${fields.map((f) => `<th>${escapeHTML(f.label)}</th>`).join("")}<th>Затраты</th><th></th></tr></thead><tbody>${list.map((e) => `<tr>${fields.map((f) => `<td>${escapeHTML(f.type === "select" ? valueLabel(e.payload[f.key] ?? "—") : e.payload[f.key] ?? "—")}</td>`).join("")}<td>${fmtMoney(e.amount_rub)}</td><td><button class="btn secondary" data-edit="${e.id}">Открыть</button></td></tr>`).join("")}</tbody></table></div>${!list.length ? '<p class="muted">Записей нет. Добавьте вручную или импортируйте Excel.</p>' : ""}`;
    root
      .querySelectorAll("[data-edit]")
      .forEach(
        (b) =>
          (b.onclick = () =>
            openEntryModal(entries.find((e) => e.id === b.dataset.edit))),
      );
  };
  root.querySelector("#entry-search").oninput = paint;
  paint();
  const paging = el(`<div class="actions"><button class="btn secondary" id="entries-prev">Назад</button><span>Страница ${Math.floor((state.entryPageOffset || 0) / 200) + 1}. Поиск и сумма — на этой странице.</span><button class="btn secondary" id="entries-next">Далее</button></div>`);
  root.querySelector("#entries-table").after(paging);
  paging.querySelector("#entries-prev").disabled = !state.entryPageOffset;
  paging.querySelector("#entries-next").disabled = entries.nextOffset == null;
  paging.querySelector("#entries-prev").onclick = () => { state.entryPageOffset = Math.max(0, state.entryPageOffset - 200); renderEntries(root); };
  paging.querySelector("#entries-next").onclick = () => { state.entryPageOffset = entries.nextOffset; renderEntries(root); };
}

async function openImportDialog(directory) {
  const query = workspaceQuery();
  const endpoint = directory
    ? "/admin/directory-import"
    : `/entries/import?${query}`;
  const template = directory
    ? "/api/admin/directory-template"
    : `/api/entries/import-template?${query}`;
  const modal = el(
    `<div class="modal-backdrop"><div class="modal" role="dialog" aria-modal="true"><h2>Импорт ${directory ? "справочника" : "записей"} из Excel</h2><p>Первый лист .xlsx, до 8 МБ и ${directory ? "10000" : "1000"} строк. Сначала скачайте шаблон. Формулы замените значениями. ${directory ? "Укажите источник и дату актуальности." : "Наставник должен заранее присутствовать в справочнике выбранного партнёра. Суммы рассчитывает сервер."}</p><a class="btn secondary" href="${template}">Скачать шаблон</a><div class="field"><input type="file" accept=".xlsx" id="import-file" aria-label="Файл Excel"></div><div id="import-result" role="status"></div><div class="flex"><button class="btn" id="preview">Проверить</button><button class="btn" id="commit" disabled>Импортировать</button><button class="btn secondary" id="close-import">Закрыть</button></div></div></div>`,
  );
  let busy = false,
    checkedFile = null;
  const file = modal.querySelector("#import-file"),
    resultBox = modal.querySelector("#import-result"),
    commit = modal.querySelector("#commit");
  const close = () => {
    if (!busy) modal.remove();
  };
  modal.querySelector("#close-import").onclick = close;
  file.onchange = () => {
    checkedFile = null;
    commit.disabled = true;
    resultBox.textContent = "";
  };
  const run = async (save) => {
    if (busy) return;
    const selected = file.files[0];
    if (!selected) {
      showToast("Выберите файл");
      return;
    }
    if (save && checkedFile !== selected) return;
    busy = true;
    modal.querySelectorAll("button,input").forEach((b) => (b.disabled = true));
    try {
      const body = new FormData();
      body.append("file", selected);
      const result = await api(
        endpoint +
          (endpoint.includes("?") ? "&" : "?") +
          `commit=${save ? "1" : "0"}`,
        { method: "POST", body },
      );
      checkedFile = result.errors.length ? null : selected;
      resultBox.innerHTML = result.errors.length
        ? `<div class="error">${result.errors.map(escapeHTML).join("<br>")}</div>`
        : `<p>${result.committed ? "Импортировано" : "Готово к импорту"}: ${result.count ?? result.rows.length} строк. ${directory ? "" : fmtMoney(result.total_rub)}</p>`;
      if (!directory && result.rows?.length)
        resultBox.innerHTML += `<div class="table-wrap"><table><thead><tr><th>Строка</th><th>Расчёт</th></tr></thead><tbody>${result.rows
          .slice(0, 20)
          .map(
            (r) =>
              `<tr><td>${r.row}</td><td>${fmtMoney(r.amount_rub)}</td></tr>`,
          )
          .join(
            "",
          )}</tbody></table></div><p class="muted">Показаны первые 20 строк.</p>`;
      if (result.committed) {
        checkedFile = null;
        showToast("Импорт завершён", "success");
        if (!directory) renderEntries(document.getElementById("content"));
      }
    } catch (e) {
      checkedFile = null;
      resultBox.textContent = e.message;
    } finally {
      busy = false;
      modal
        .querySelectorAll("button,input")
        .forEach((b) => (b.disabled = false));
      commit.disabled = !checkedFile;
    }
  };
  modal.querySelector("#preview").onclick = () => run(false);
  commit.onclick = () => run(true);
  document.body.appendChild(modal);
}

async function renderPartnerDirectory(root) {
  root.innerHTML = `<section class="page-heading"><div><span class="eyebrow">Справочники</span><h1>Учебные заведения</h1><p>Партнёры Киберпротекта — только образовательные организации. Наличие в справочнике не означает наличие соглашения.</p></div></section>
  <div class="card"><h2>Поиск в справочнике</h2><p class="muted">Начальное наполнение: 1 257 вузов и филиалов — участников мониторинга ВО 2025. Источник указан у каждой записи. Это не полный реестр лицензий; школы и СПО добавляются отдельно вручную или импортом.</p><div class="grid cols-3"><div class="field"><label>Тип ОО</label><select id="d-kind">${Object.entries(
    AUDIENCE_LABELS,
  )
    .map(
      ([k, v]) =>
        `<option value="${k}" ${k === state.partnerKind ? "selected" : ""}>${v}</option>`,
    )
    .join(
      "",
    )}</select></div><div class="field"><label>Название или регион</label><input id="d-search" placeholder="Поиск"></div><button class="btn secondary" id="d-find">Найти</button></div><div id="d-results"></div>${state.me.role === "admin" ? '<button class="btn secondary" id="d-import">Загрузить справочник из Excel</button>' : ""}</div>
  <div class="card"><h2>Наши партнёры</h2><div id="partner-list"></div></div>${isStaffUser() ? '<div id="partner-create"></div>' : ""}`;
  let generation = 0;
  const search = async () => {
    const version = ++generation;
    const box = root.querySelector("#d-results");
    box.textContent = "Поиск…";
    try {
      const items = await api(
        "/directory?" +
          new URLSearchParams({
            partner_kind: root.querySelector("#d-kind").value,
            q: root.querySelector("#d-search").value,
          }),
      );
      if (version !== generation) return;
      box.innerHTML = items.length
        ? `<p class="muted">Показано до 100 результатов. Уточните запрос для поиска остальных.</p><div class="table-wrap"><table><thead><tr><th>Название</th><th>Регион / источник</th><th></th></tr></thead><tbody>${items.map((p) => `<tr><td>${escapeHTML(p.name)}</td><td>${escapeHTML(p.region)}<br><small>${escapeHTML(p.source)}</small></td><td>${isStaffUser() ? `<button class="btn secondary" data-directory="${p.id}">Выбрать</button>` : ""}</td></tr>`).join("")}</tbody></table></div>`
        : "<p>Совпадений нет. Администратор может импортировать справочник; сотрудник — добавить партнёра вручную ниже.</p>";
      box.querySelectorAll("[data-directory]").forEach(
        (b) =>
          (b.onclick = () => {
            const item = items.find((i) => i.id === b.dataset.directory);
            const form = root.querySelector("#partner-create");
            form.dataset.directoryId = item.id;
            form.querySelector("#p-name").value = item.name;
            form.querySelector("#p-kind").value = item.partner_kind;
            form.querySelector("#p-name").readOnly = true;
            form.querySelector("#p-kind").disabled = true;
            form.scrollIntoView({ behavior: "smooth" });
          }),
      );
    } catch (e) {
      if (version === generation) box.textContent = e.message;
    }
  };
  root.querySelector("#d-find").onclick = search;
  root.querySelector("#d-kind").onchange = () => {
    state.partnerKind = root.querySelector("#d-kind").value;
    search();
  };
  root.querySelector("#d-search").onkeydown = (e) => {
    if (e.key === "Enter") search();
  };
  root
    .querySelector("#d-import")
    ?.addEventListener("click", () => openImportDialog(true));
  const refreshPartners = async () => {
    state.partners = await api("/partners");
    root.querySelector("#partner-list").innerHTML =
      `<div class="table-wrap"><table><thead><tr><th>Учебное заведение</th><th>Тип</th><th>Соглашение</th><th></th></tr></thead><tbody>${state.partners.map((p) => `<tr><td>${escapeHTML(p.name)}</td><td>${escapeHTML(AUDIENCE_LABELS[p.partner_kind])}</td><td>${escapeHTML(p.agreement_number || "—")}<br>${escapeHTML(p.agreement_date || "")}</td><td><button class="btn secondary" data-partner="${p.id}">План / факт</button><button class="btn secondary" data-mentors="${p.id}">Наставники</button></td></tr>`).join("")}</tbody></table></div>`;
    root.querySelectorAll("[data-partner]").forEach(
      (b) =>
        (b.onclick = () => {
          state.partnerID = b.dataset.partner;
          state.view = "entries";
          render();
        }),
    );
    root
      .querySelectorAll("[data-mentors]")
      .forEach((b) => (b.onclick = () => openMentors(b.dataset.mentors)));
  };
  if (isStaffUser()) {
    const box = root.querySelector("#partner-create");
    box.innerHTML = `<form class="card" id="p-form"><h2>Добавить учебное заведение в партнёры</h2><div class="grid cols-3"><div class="field"><label>Название *</label><input id="p-name" maxlength="1000" required></div><div class="field"><label>Тип ОО</label><select id="p-kind">${Object.entries(
      AUDIENCE_LABELS,
    )
      .map(([k, v]) => `<option value="${k}">${v}</option>`)
      .join(
        "",
      )}</select></div><div class="field"><label>Дата соглашения</label><input type="date" id="p-date"></div><div class="field"><label>Номер соглашения</label><input id="p-number" maxlength="100"></div><div class="field"><label>Другие соглашения</label><input id="p-other" maxlength="1000"></div></div><button class="btn" type="submit">Добавить партнёра</button><button class="btn secondary" type="reset">Ввести вручную</button><p id="p-error" class="error" role="alert"></p></form>`;
    box.querySelector("form").onreset = () => {
      box.dataset.directoryId = "";
      box.querySelector("#p-name").readOnly = false;
      box.querySelector("#p-kind").disabled = false;
    };
    box.querySelector("form").onsubmit = async (e) => {
      e.preventDefault();
      const button = box.querySelector("[type=submit]");
      button.disabled = true;
      try {
        await api("/partners", {
          method: "POST",
          body: JSON.stringify({
            directory_id: box.dataset.directoryId || "",
            name: box.querySelector("#p-name").value,
            partner_kind: box.querySelector("#p-kind").value,
            agreement_date: box.querySelector("#p-date").value,
            agreement_number: box.querySelector("#p-number").value,
            other_agreement: box.querySelector("#p-other").value,
          }),
        });
        e.target.reset();
        await refreshPartners();
        showToast("Партнёр добавлен", "success");
        box.querySelector("#p-error").textContent = "";
      } catch (error) {
        box.querySelector("#p-error").textContent = error.message;
      } finally {
        button.disabled = false;
      }
    };
  }
  await Promise.all([search(), refreshPartners()]);
}

async function openMentors(partnerID) {
  const modal = el(
    `<div class="modal-backdrop"><div class="modal" role="dialog" aria-modal="true"><h2>Наставники: ${escapeHTML(partnerName(partnerID))}</h2><div id="mentor-list"></div><form><div class="field"><label>Фамилия, имя, отчество при наличии *</label><input name="full_name" required maxlength="200"></div><button class="btn" type="submit">Добавить наставника</button><button class="btn secondary" type="button" id="mentor-close">Закрыть</button><p class="error" role="alert"></p></form></div></div>`,
  );
  const refresh = async () => {
    const list = await api(`/mentors?partner_id=${partnerID}`);
    modal.querySelector("#mentor-list").innerHTML = list.length
      ? `<p>Отчёты по наставнику за ${state.year} год:</p><ul>${list.map((m) => `<li>${escapeHTML(m.full_name)} — <a href="/api/reports/export?partner_id=${partnerID}&mentor_id=${m.id}&report_year=${state.year}&period_type=plan">План Excel</a> / <a href="/api/reports/export?partner_id=${partnerID}&mentor_id=${m.id}&report_year=${state.year}&period_type=fact">Факт Excel</a></li>`).join("")}</ul>`
      : "<p>Наставников пока нет</p>";
  };
  modal.querySelector("#mentor-close").onclick = () => modal.remove();
  modal.querySelector("form").onsubmit = async (e) => {
    e.preventDefault();
    const button = modal.querySelector("[type=submit]");
    button.disabled = true;
    try {
      await api("/mentors", {
        method: "POST",
        body: JSON.stringify({
          partner_id: partnerID,
          full_name: e.target.elements.full_name.value,
        }),
      });
      e.target.reset();
      await refresh();
    } catch (err) {
      modal.querySelector(".error").textContent = err.message;
    } finally {
      button.disabled = false;
    }
  };
  document.body.appendChild(modal);
  try {
    await refresh();
  } catch (e) {
    modal.querySelector(".error").textContent = e.message;
  }
}
