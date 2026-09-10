// Partner-first workflows. Formula fields come from the backend registry.
function isStaffUser() {
  return state.me?.role === "admin" || state.me?.entity_type === "organization";
}

function agreementIsUsable(agreement, year = state.year) {
  return Boolean(
    agreement &&
      agreement.status === "active" &&
      agreement.valid_from <= `${year}-12-31` &&
      agreement.valid_until >= `${year}-01-01`,
  );
}

function agreementLabel(agreement) {
  if (!agreement) return "—";
  const authority = agreement.roiv_name ? ` · ${agreement.roiv_name}` : "";
  return `№ ${agreement.number}${authority} · ${AGREEMENT_STATUS_LABELS[agreement.status] || agreement.status} · ${agreement.valid_from}—${agreement.valid_until}`;
}

function peopleToText(agreement, party) {
  return (agreement?.responsible_people || [])
    .filter((person) => person.party === party)
    .map((person) =>
      [person.full_name, person.position, person.email, person.phone]
        .map((value) => String(value || "").replaceAll("|", "/"))
        .join(" | "),
    )
    .join("\n");
}

function agreementFieldsMarkup(prefix, agreement = {}, withPartners = false) {
  const today = new Date().toISOString().slice(0, 10);
  const yearEnd = `${new Date().getFullYear()}-12-31`;
  return `<div class="grid cols-3">
    <div class="field"><label>Тип соглашения *</label><select id="${prefix}-kind"><option value="education_organization">С образовательной организацией</option><option value="roiv">С РОИВ</option></select></div>
    <div class="field"><label>Номер *</label><input id="${prefix}-number" required maxlength="100" value="${escapeHTML(agreement.number || "")}"></div>
    <div class="field"><label>Статус *</label><select id="${prefix}-status"><option value="draft">Проект</option><option value="active">Действует</option><option value="suspended">Приостановлено</option><option value="expired">Истекло</option><option value="terminated">Расторгнуто</option></select></div>
    <div class="field"><label>Дата соглашения *</label><input type="date" id="${prefix}-signed-on" required value="${escapeHTML(agreement.signed_on || today)}"></div>
    <div class="field"><label>Действует с *</label><input type="date" id="${prefix}-valid-from" required value="${escapeHTML(agreement.valid_from || today)}"></div>
    <div class="field"><label>Действует до *</label><input type="date" id="${prefix}-valid-until" required value="${escapeHTML(agreement.valid_until || yearEnd)}"></div>
    <div class="field" id="${prefix}-roiv-box"><label>Региональный орган управления образованием *</label><select id="${prefix}-roiv-authority"><option value="">— Выберите РОИВ —</option>${state.regionalAuthorities.map((authority) => `<option value="${authority.id}" ${authority.id === agreement.regional_authority_id ? "selected" : ""} ${authority.status === "active" ? "" : "disabled"}>${escapeHTML(authority.region)} — ${escapeHTML(authority.name)}</option>`).join("")}</select><div class="field-hint">РОИВ ведётся отдельно и связывает соглашение со школами и их мероприятиями.</div></div>
    <div class="field"><label>Группа юридических лиц</label><input id="${prefix}-group" maxlength="1000" value="${escapeHTML(agreement.legal_entity_group || "")}" placeholder="Название группы или периметра соглашения"></div>
    <div class="field"><label>Способ подписания *</label><select id="${prefix}-signature"><option value="unsigned">Не подписано</option><option value="paper">Бумажный документ</option><option value="qualified_electronic">УКЭП</option><option value="goskey">Госключ</option></select></div>
    <div class="field"><label>Подписант (обязательно для действующего)</label><input id="${prefix}-signed-by" maxlength="300" value="${escapeHTML(agreement.signed_by || "")}"></div>
    <div class="field"><label>Дата подписания (обязательно для действующего)</label><input type="date" id="${prefix}-signature-date" value="${escapeHTML(agreement.signature_date || "")}"></div>
    <div class="field"><label>Ссылка / реквизиты документа</label><input id="${prefix}-document" maxlength="1000" value="${escapeHTML(agreement.document_reference || "")}"></div>
  </div>
  ${withPartners ? `<div class="field"><label>Учебные заведения, охваченные соглашением *</label><select id="${prefix}-partners" multiple size="5" required>${state.partners.map((partner) => `<option value="${partner.id}" data-kind="${partner.partner_kind}" ${(agreement.partner_ids || []).includes(partner.id) ? "selected" : ""}>${escapeHTML(partner.name)} (${escapeHTML(AUDIENCE_LABELS[partner.partner_kind])})</option>`).join("")}</select><div class="field-hint">Соглашение с РОИВ охватывает одну или несколько школ. Для вузов и СПО используется соглашение с образовательной организацией.</div></div>` : ""}
  <div class="grid cols-2">
    <div class="field"><label>Ответственные Киберпротекта</label><textarea id="${prefix}-people-cp" rows="3" placeholder="ФИО | должность | email | телефон">${escapeHTML(peopleToText(agreement, "cyberprotect"))}</textarea></div>
    <div class="field"><label>Ответственные контрагента</label><textarea id="${prefix}-people-other" rows="3" placeholder="ФИО | должность | email | телефон">${escapeHTML(peopleToText(agreement, "counterparty"))}</textarea></div>
  </div><div class="field-hint">Одно ответственное лицо на строку. Для действующего соглашения укажите хотя бы по одному с каждой стороны.</div>
  <div class="field"><label>Примечание</label><textarea id="${prefix}-notes" rows="2" maxlength="1000">${escapeHTML(agreement.notes || "")}</textarea></div>`;
}

function wireAgreementFields(root, prefix, agreement = {}) {
  const kind = root.querySelector(`#${prefix}-kind`);
  const status = root.querySelector(`#${prefix}-status`);
  const signature = root.querySelector(`#${prefix}-signature`);
  kind.value = agreement.agreement_kind || "education_organization";
  status.value = agreement.status === "needs_review" ? "draft" : agreement.status || "active";
  signature.value = agreement.signature_method || "paper";
  const sync = () => {
    const roiv = root.querySelector(`#${prefix}-roiv-authority`);
    const active = status.value === "active";
    root.querySelector(`#${prefix}-roiv-box`).hidden = kind.value !== "roiv";
    roiv.required = kind.value === "roiv";
    const partners = root.querySelector(`#${prefix}-partners`);
    if (partners) {
      [...partners.options].forEach((option) => {
        const allowed =
          kind.value === "roiv"
            ? option.dataset.kind === "school"
            : option.dataset.kind !== "school";
        option.disabled = !allowed;
        if (!allowed) option.selected = false;
      });
    }
    root.querySelector(`#${prefix}-signed-by`).required = active;
    root.querySelector(`#${prefix}-signature-date`).required = active;
    root.querySelector(`#${prefix}-people-cp`).required = active;
    root.querySelector(`#${prefix}-people-other`).required = active;
    signature.setCustomValidity(
      active && signature.value === "unsigned"
        ? "Для действующего соглашения выберите способ подписания"
        : "",
    );
  };
  kind.onchange = sync;
  status.onchange = sync;
  signature.onchange = sync;
  sync();
}

function parseResponsiblePeople(text, party) {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [full_name = "", position = "", email = "", phone = ""] = line
        .split("|")
        .map((value) => value.trim());
      return { party, full_name, position, email, phone };
    });
}

function collectAgreement(root, prefix, partnerIDs) {
  const selectedPartners = root.querySelector(`#${prefix}-partners`);
  const ids = selectedPartners
    ? [...selectedPartners.selectedOptions].map((option) => option.value)
    : partnerIDs;
  return {
    partner_ids: ids,
    agreement_kind: root.querySelector(`#${prefix}-kind`).value,
    number: root.querySelector(`#${prefix}-number`).value.trim(),
    status: root.querySelector(`#${prefix}-status`).value,
    signed_on: root.querySelector(`#${prefix}-signed-on`).value,
    valid_from: root.querySelector(`#${prefix}-valid-from`).value,
    valid_until: root.querySelector(`#${prefix}-valid-until`).value,
    regional_authority_id: root.querySelector(
      `#${prefix}-roiv-authority`,
    ).value,
    legal_entity_group: root.querySelector(`#${prefix}-group`).value.trim(),
    signature_method: root.querySelector(`#${prefix}-signature`).value,
    signed_by: root.querySelector(`#${prefix}-signed-by`).value.trim(),
    signature_date: root.querySelector(`#${prefix}-signature-date`).value,
    document_reference: root.querySelector(`#${prefix}-document`).value.trim(),
    notes: root.querySelector(`#${prefix}-notes`).value.trim(),
    responsible_people: [
      ...parseResponsiblePeople(root.querySelector(`#${prefix}-people-cp`).value, "cyberprotect"),
      ...parseResponsiblePeople(root.querySelector(`#${prefix}-people-other`).value, "counterparty"),
    ],
  };
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
    agreement_id: state.agreementID,
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
  if (partner && state.agreementPartnerID !== partner.id) {
    state.agreements = await api(
      `/agreements?partner_id=${encodeURIComponent(partner.id)}`,
    );
    state.agreementPartnerID = partner.id;
    state.agreementID = "";
  }
  if (!partner) {
    state.agreements = [];
    state.agreementPartnerID = "";
    state.agreementID = "";
  }
  if (
    !state.agreements.some((agreement) => agreement.id === state.agreementID)
  ) {
    state.agreementID =
      state.agreements.find((agreement) => agreementIsUsable(agreement))?.id ||
      state.agreements[0]?.id ||
      "";
  }
  const selectedAgreement = state.agreements.find(
    (agreement) => agreement.id === state.agreementID,
  );
  const writable = agreementIsUsable(selectedAgreement);
  const available = state.categories.filter((c) =>
    c.audience_scope.includes(partner?.partner_kind || state.partnerKind),
  );
  if (!available.some((c) => c.code === state.categoryCode))
    state.categoryCode = available[0]?.code || "";
  root.innerHTML = `<section class="page-heading"><div><span class="eyebrow">Работа с партнёром</span><h1>План и факт</h1><p>Учебное заведение → действующее соглашение → доступная активность → расчёт и необязательные вложения.</p></div></section>
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
    <div class="field"><label for="workspace-agreement">3. Соглашение</label><select id="workspace-agreement"><option value="">— Выберите —</option>${state.agreements.map((agreement) => `<option value="${agreement.id}" ${agreement.id === state.agreementID ? "selected" : ""}>${escapeHTML(agreementLabel(agreement))}</option>`).join("")}</select></div>
  </div><button class="btn secondary" id="open-directory">Справочник и соглашения</button><p class="muted">${selectedAgreement ? `${escapeHTML(AGREEMENT_KIND_LABELS[selectedAgreement.agreement_kind] || selectedAgreement.agreement_kind)}. ${writable ? "Можно вносить план/факт за выбранный год." : "Просмотр доступен, но для ввода нужен статус «Действует» и период, охватывающий выбранный год."}` : "Сначала выберите партнёра и соглашение. Нового партнёра добавляет сотрудник Киберпротекта."}</p></div>
  <div class="card"><div class="tabs"><button data-p="plan" class="${state.period === "plan" ? "active" : ""}">План</button><button data-p="fact" class="${state.period === "fact" ? "active" : ""}">Факт</button></div>
    <div class="grid cols-3"><div class="field"><label>Год</label><input type="number" id="year" min="2000" max="2100" step="1" value="${state.year}"></div>
    <div class="field"><label>4. Категория активности</label><select id="category">${available.map((c) => `<option value="${c.code}" ${c.code === state.categoryCode ? "selected" : ""}>${escapeHTML(c.name)}</option>`).join("")}</select></div>
    <div class="field"><label>Действия</label><button class="btn" id="add-entry" ${writable ? "" : "disabled"}>+ Добавить запись</button></div></div>
    <div class="flex"><button class="btn secondary" id="import-entries" ${writable ? "" : "disabled"}>Импорт из Excel</button><a class="btn secondary" id="export-link">Excel: категория</a><a class="btn secondary" id="export-all-link">Excel: все активности партнёра</a><a class="btn secondary" id="export-word">Word: таблица</a></div>
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
    state.agreementID = "";
    state.agreementPartnerID = "";
    renderEntries(root);
  };
  partnerSelect.onchange = (e) => {
    state.partnerID = e.target.value;
    state.agreementID = "";
    state.agreementPartnerID = "";
    renderEntries(root);
  };
  root.querySelector("#workspace-agreement").onchange = (e) => {
    state.agreementID = e.target.value;
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
    `<div class="modal-backdrop"><div class="modal" role="dialog" aria-modal="true"><h2>Импорт ${directory ? "справочника" : "записей"} из Excel</h2><p>Первый лист .xlsx, до 8 МБ и ${directory ? "10000" : "1000"} строк. Сначала скачайте шаблон. Формулы замените значениями. ${directory ? "Для каждой записи обязательны ИНН, ОГРН, лицензия, оба статуса, идентификатор, дата и HTTPS-ссылка официального реестра." : "Наставник должен заранее присутствовать в справочнике выбранного партнёра. Суммы рассчитывает сервер."}</p><a class="btn secondary" href="${template}">Скачать шаблон</a><div class="field"><input type="file" accept=".xlsx" id="import-file" aria-label="Файл Excel"></div><div id="import-result" role="status"></div><div class="flex"><button class="btn" id="preview">Проверить</button><button class="btn" id="commit" disabled>Импортировать</button><button class="btn secondary" id="close-import">Закрыть</button></div></div></div>`,
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
        const content = document.getElementById("content");
        if (!directory) renderEntries(content);
        else if (content) renderPartnerDirectory(content);
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

function openRegionalAuthority(authority, onSaved) {
  const editing = Boolean(authority?.id);
  const modal = el(
    `<div class="modal-backdrop"><form class="modal" role="dialog" aria-modal="true"><h2>${editing ? "Изменить РОИВ" : "Добавить РОИВ"}</h2>
      <div class="field"><label>Полное наименование *</label><input name="name" required minlength="2" maxlength="300" value="${escapeHTML(authority?.name || "")}"></div>
      <div class="field"><label>Субъект Российской Федерации *</label><input name="region" required minlength="2" maxlength="200" value="${escapeHTML(authority?.region || "")}"></div>
      <div class="grid cols-2"><div class="field"><label>ИНН *</label><input name="inn" required inputmode="numeric" pattern="[0-9]{10}|[0-9]{12}" value="${escapeHTML(authority?.inn || "")}"></div><div class="field"><label>ОГРН / ОГРНИП *</label><input name="ogrn" required inputmode="numeric" pattern="[0-9]{13}|[0-9]{15}" value="${escapeHTML(authority?.ogrn || "")}"></div></div>
      <div class="field"><label>Статус *</label><select name="status"><option value="active">Действует</option><option value="inactive">Не действует</option></select></div>
      <div class="field"><label>Официальный источник *</label><input name="source_url" type="url" required placeholder="https://...gov.ru" value="${escapeHTML(authority?.source_url || "")}"></div>
      <p class="field-hint">ИНН и ОГРН проверяются по контрольным суммам. Разрешены только HTTPS-ссылки официальных доменов.</p>
      <p class="error" role="alert"></p><div class="flex"><button class="btn" type="submit">Сохранить</button><button class="btn secondary" type="button">Отмена</button></div>
    </form></div>`,
  );
  const form = modal.querySelector("form");
  form.elements.status.value = authority?.status || "active";
  form.querySelector("[type=button]").onclick = () => modal.remove();
  form.onsubmit = async (event) => {
    event.preventDefault();
    const button = form.querySelector("[type=submit]");
    const error = form.querySelector(".error");
    button.disabled = true;
    error.textContent = "";
    try {
      await api(
        editing ? `/regional-authorities/${authority.id}` : "/regional-authorities",
        {
          method: editing ? "PUT" : "POST",
          body: JSON.stringify({
            name: form.elements.name.value.trim(),
            region: form.elements.region.value.trim(),
            inn: form.elements.inn.value.trim(),
            ogrn: form.elements.ogrn.value.trim(),
            status: form.elements.status.value,
            source_url: form.elements.source_url.value.trim(),
          }),
        },
      );
      modal.remove();
      await onSaved();
      showToast("РОИВ сохранён", "success");
    } catch (err) {
      error.textContent = err.message;
    } finally {
      button.disabled = false;
    }
  };
  document.body.appendChild(modal);
}

async function renderPartnerDirectory(root) {
  state.regionalAuthorities = await api("/regional-authorities");
  root.innerHTML = `<section class="page-heading"><div><span class="eyebrow">Справочники</span><h1>Учебные заведения и соглашения</h1><p>Новые партнёры создаются только из записей, подтверждённых официальным реестром лицензий.</p></div></section>
  <div class="card"><h2>Состояние справочника</h2><div id="directory-stats">Загрузка…</div><p class="muted">Старый набор мониторинга сохранён для истории, но не считается лицензированным реестром и не доступен для создания нового партнёра. ИНН и ОГРН проверяются по контрольным суммам; запись должна иметь действующие статусы организации и лицензии.</p></div>
  <div class="card"><h2>Поиск в официальном справочнике</h2><div class="grid cols-3"><div class="field"><label>Тип ОО</label><select id="d-kind">${Object.entries(
    AUDIENCE_LABELS,
  )
    .map(
      ([k, v]) =>
        `<option value="${k}" ${k === state.partnerKind ? "selected" : ""}>${v}</option>`,
    )
    .join(
      "",
    )}</select></div><div class="field"><label>Название, регион, ИНН, ОГРН или лицензия</label><input id="d-search" placeholder="Поиск"></div><button class="btn secondary" id="d-find">Найти</button></div><div id="d-results"></div>${state.me.role === "admin" ? '<button class="btn secondary" id="d-import">Обновить из выгрузки реестра</button>' : ""}</div>
  <div class="card"><div class="flex between"><div><h2>Региональные органы управления образованием</h2><p class="muted">Цепочка школьного мероприятия: РОИВ → соглашение → школа → план/факт.</p></div>${isStaffUser() ? '<button class="btn secondary" id="roiv-add">+ Добавить РОИВ</button>' : ""}</div><div id="roiv-list">${state.regionalAuthorities.length ? `<div class="table-wrap"><table><thead><tr><th>Регион и РОИВ</th><th>Реквизиты</th><th>Связи</th><th></th></tr></thead><tbody>${state.regionalAuthorities.map((authority) => `<tr><td>${escapeHTML(authority.region)}<br><b>${escapeHTML(authority.name)}</b></td><td>ИНН ${escapeHTML(authority.inn)}<br>ОГРН ${escapeHTML(authority.ogrn)}<br><a href="${escapeHTML(authority.source_url)}" target="_blank" rel="noopener noreferrer">Официальный источник</a></td><td><span class="status-badge ${authority.status === "active" ? "active" : "inactive"}">${authority.status === "active" ? "Действует" : "Не действует"}</span><br>Школ: ${authority.schools_count}<br>Мероприятий: ${authority.activities_count}</td><td>${isStaffUser() ? `<button class="btn secondary" data-edit-roiv="${authority.id}">Изменить</button>` : ""}</td></tr>`).join("")}</tbody></table></div>` : "<p>РОИВ ещё не добавлены. Для создания школьного партнёра сначала заполните этот справочник.</p>"}</div></div>
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
        ? `<p class="muted">Показано до 100 результатов. Уточните запрос для поиска остальных.</p><div class="table-wrap"><table><thead><tr><th>Название</th><th>Реквизиты</th><th>Лицензия и актуальность</th><th></th></tr></thead><tbody>${items.map((p) => `<tr><td>${escapeHTML(p.name)}<br><small>${escapeHTML(p.region)}</small></td><td>ИНН ${escapeHTML(p.inn || "—")}<br>ОГРН ${escapeHTML(p.ogrn || "—")}</td><td><span class="status-badge ${p.selectable ? "active" : "inactive"}">${p.selectable ? "Подтверждено" : "Не подтверждено"}</span><br>${escapeHTML(p.license_number || "Лицензия не указана")} · ${escapeHTML(p.license_status)}<br><small>${escapeHTML(p.registry_updated_at || p.source)}</small></td><td>${isStaffUser() ? `<button class="btn secondary" data-directory="${p.id}" ${p.selectable ? "" : "disabled"}>${p.selectable ? "Выбрать" : "Недоступно"}</button>` : ""}${p.source_url ? `<br><a href="${escapeHTML(p.source_url)}" target="_blank" rel="noopener noreferrer">Источник</a>` : ""}</td></tr>`).join("")}</tbody></table></div>`
        : "<p>Совпадений нет. Администратор должен обновить справочник официальной выгрузкой — произвольный ручной ввод отключён.</p>";
      box.querySelectorAll("[data-directory]").forEach(
        (b) =>
          (b.onclick = () => {
            const item = items.find((i) => i.id === b.dataset.directory);
            const form = root.querySelector("#partner-create");
            form.dataset.directoryId = item.id;
            form.querySelector("#p-selected").innerHTML =
              `<b>${escapeHTML(item.name)}</b><br>ИНН ${escapeHTML(item.inn)} · ` +
              `ОГРН ${escapeHTML(item.ogrn)} · лицензия ${escapeHTML(item.license_number)}`;
            form.querySelector("[type=submit]").disabled = false;
            const kind = form.querySelector("#p-a-kind");
            kind.value =
              item.partner_kind === "school"
                ? "roiv"
                : "education_organization";
            kind.dispatchEvent(new Event("change"));
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
  root
    .querySelector("#roiv-add")
    ?.addEventListener("click", () =>
      openRegionalAuthority(null, () => renderPartnerDirectory(root)),
    );
  root.querySelectorAll("[data-edit-roiv]").forEach((button) => {
    button.onclick = () =>
      openRegionalAuthority(
        state.regionalAuthorities.find(
          (item) => item.id === button.dataset.editRoiv,
        ),
        () => renderPartnerDirectory(root),
      );
  });
  api("/directory/stats")
    .then((stats) => {
      const box = root.querySelector("#directory-stats");
      if (!box) return;
      box.innerHTML = `<div class="grid cols-3"><div class="stat"><div class="label">Всего записей</div><div class="value">${stats.total}</div></div><div class="stat"><div class="label">Подтверждены и действуют</div><div class="value">${stats.verified_active}</div></div><div class="stat"><div class="label">Вузы / СПО / школы</div><div class="value">${stats.universities} / ${stats.colleges} / ${stats.schools}</div></div></div><p class="muted">Последняя проверка: ${stats.last_verified_at ? new Date(stats.last_verified_at).toLocaleString("ru-RU") : "официальная выгрузка ещё не загружена"}. ${stats.sync_status ? `Автообновление: ${escapeHTML(stats.sync_status)}${stats.sync_finished_at ? `, ${new Date(stats.sync_finished_at).toLocaleString("ru-RU")}` : ""}${stats.sync_error ? ` — ${escapeHTML(stats.sync_error)}` : ""}.` : "Автообновление включается переменной DIRECTORY_SYNC_URL."}</p>`;
    })
    .catch((error) => {
      const box = root.querySelector("#directory-stats");
      if (box) box.textContent = error.message;
    });
  const refreshPartners = async () => {
    state.partners = await api("/partners");
    root.querySelector("#partner-list").innerHTML =
      `<div class="table-wrap"><table><thead><tr><th>Учебное заведение</th><th>Проверка</th><th>Соглашения</th><th></th></tr></thead><tbody>${state.partners.map((p) => `<tr><td>${escapeHTML(p.name)}<br><small>${escapeHTML(AUDIENCE_LABELS[p.partner_kind])}</small></td><td><span class="status-badge ${p.verification_status === "verified" ? "active" : "inactive"}">${p.verification_status === "verified" ? "Реестр подтверждён" : "Историческая запись"}</span><br><small>${escapeHTML(p.inn || "ИНН не указан")}</small></td><td>Всего: ${p.agreements_count}<br>Действующих сейчас: ${p.active_agreements_count}</td><td><button class="btn secondary" data-partner="${p.id}">План / факт</button><button class="btn secondary" data-agreements="${p.id}">Соглашения</button><button class="btn secondary" data-mentors="${p.id}">Наставники</button></td></tr>`).join("")}</tbody></table></div>`;
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
    root.querySelectorAll("[data-agreements]").forEach(
      (button) =>
        (button.onclick = () =>
          openAgreements(button.dataset.agreements, refreshPartners)),
    );
  };
  if (isStaffUser()) {
    const box = root.querySelector("#partner-create");
    box.innerHTML = `<form class="card" id="p-form"><h2>Новый партнёр и первое соглашение</h2><p id="p-selected" class="notice">Сначала найдите выше подтверждённую организацию и нажмите «Выбрать».</p>${agreementFieldsMarkup("p-a")}<button class="btn" type="submit" disabled>Создать партнёра и соглашение</button><p id="p-error" class="error" role="alert"></p></form>`;
    wireAgreementFields(box, "p-a");
    box.querySelector("form").onsubmit = async (e) => {
      e.preventDefault();
      const button = box.querySelector("[type=submit]");
      button.disabled = true;
      try {
        await api("/partners", {
          method: "POST",
          body: JSON.stringify({
            directory_id: box.dataset.directoryId || "",
            initial_agreement: collectAgreement(box, "p-a", []),
          }),
        });
        box.dataset.directoryId = "";
        await renderPartnerDirectory(root);
        showToast("Партнёр добавлен", "success");
      } catch (error) {
        box.querySelector("#p-error").textContent = error.message;
      } finally {
        button.disabled = false;
      }
    };
  }
  await Promise.all([search(), refreshPartners()]);
}

async function openAgreements(partnerID, onSaved = async () => {}) {
  state.regionalAuthorities = await api("/regional-authorities");
  const modal = el(`<div class="modal-backdrop"><div class="modal modal-wide" role="dialog" aria-modal="true"><div class="flex between"><h2>Соглашения: ${escapeHTML(partnerName(partnerID))}</h2><button class="btn secondary" id="agreement-close">Закрыть</button></div><div id="agreement-list">Загрузка…</div>${isStaffUser() ? `<form id="agreement-form"><h2 id="agreement-form-title">Добавить соглашение</h2>${agreementFieldsMarkup("agreement", {partner_ids: [partnerID]}, true)}<div class="flex"><button class="btn" type="submit">Сохранить соглашение</button><button class="btn secondary" type="button" id="agreement-reset">Новое</button></div><p class="error" role="alert"></p></form>` : ""}</div></div>`);
  document.body.appendChild(modal);
  modal.querySelector("#agreement-close").onclick = () => modal.remove();
  const form = modal.querySelector("#agreement-form");
  let agreements = [];
  let editingID = "";
  const renderList = () => {
    modal.querySelector("#agreement-list").innerHTML = agreements.length
      ? agreements.map((agreement) => `<div class="agreement-card"><div><b>${escapeHTML(agreementLabel(agreement))}</b><br>${escapeHTML(AGREEMENT_KIND_LABELS[agreement.agreement_kind])}${agreement.roiv_name ? `: ${escapeHTML(agreement.roiv_name)}` : ""}<br><small>Подписание: ${escapeHTML(agreement.signature_method)}${agreement.signed_by ? ` · ${escapeHTML(agreement.signed_by)}` : ""}. Организаций: ${agreement.partner_ids.length}. Ответственных: ${agreement.responsible_people.length}.</small></div>${isStaffUser() ? `<button class="btn secondary" data-edit-agreement="${agreement.id}">Изменить</button>` : ""}</div>`).join("")
      : "<p>Соглашений нет.</p>";
    modal.querySelectorAll("[data-edit-agreement]").forEach((button) => {
      button.onclick = () => fillForm(agreements.find((item) => item.id === button.dataset.editAgreement));
    });
  };
  const load = async () => {
    agreements = await api(`/agreements?partner_id=${encodeURIComponent(partnerID)}`);
    renderList();
  };
  const fillForm = (agreement = { partner_ids: [partnerID] }) => {
    if (!form) return;
    editingID = agreement.id || "";
    modal.querySelector("#agreement-form-title").textContent = editingID ? `Изменить соглашение № ${agreement.number}` : "Добавить соглашение";
    const replacement = el(`<div id="agreement-fields">${agreementFieldsMarkup("agreement", agreement, true)}</div>`);
    const old = form.querySelector("#agreement-fields");
    if (old) old.replaceWith(replacement);
    else form.querySelector("h2").after(replacement);
    wireAgreementFields(form, "agreement", agreement);
  };
  if (form) {
    const initialFields = document.createElement("div");
    initialFields.id = "agreement-fields";
    while (form.children[1] && !form.children[1].classList?.contains("flex") && form.children[1].tagName !== "P") initialFields.appendChild(form.children[1]);
    form.querySelector("h2").after(initialFields);
    wireAgreementFields(form, "agreement", { partner_ids: [partnerID] });
    form.querySelector("#agreement-reset").onclick = () => fillForm();
    form.onsubmit = async (event) => {
      event.preventDefault();
      const button = form.querySelector("[type=submit]");
      const error = form.querySelector(".error");
      button.disabled = true;
      error.textContent = "";
      try {
        const body = collectAgreement(form, "agreement", [partnerID]);
        await api(editingID ? `/agreements/${editingID}` : "/agreements", { method: editingID ? "PUT" : "POST", body: JSON.stringify(body) });
        await load();
        await onSaved();
        fillForm();
        state.agreementPartnerID = "";
        showToast("Соглашение сохранено", "success");
      } catch (err) {
        error.textContent = err.message;
      } finally {
        button.disabled = false;
      }
    };
  }
  try { await load(); } catch (error) { modal.querySelector("#agreement-list").textContent = error.message; }
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
