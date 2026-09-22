// Partner-first workflows. Formula fields come from the backend registry.
function isStaffUser() {
  return state.me?.entity_type === "organization" && ["super_admin", "holding_admin", "org_admin", "curator"].includes(state.me?.role);
}

function isEducationReviewer() {
  return state.me?.entity_type === "edu_institution";
}

function canManageITCompanies() {
  return ["super_admin", "holding_admin"].includes(state.me?.role);
}

function canViewITCompanies() {
  return Boolean(state.me?.role);
}

function canReviewEducationDirectory() {
  if (state.me?.entity_type === "organization") return true;
  return state.me?.role === "curator" && state.me?.entity_type === "edu_institution";
}

function canProposeEducationDirectory() {
  return state.me?.role === "org_admin" && state.me?.entity_type === "organization";
}

function canApproveEducationDirectory() {
  return state.me?.role === "super_admin" && state.me?.entity_type === "organization";
}

function canManageAcademicStructure() {
  return isStaffUser() || (state.me?.entity_type === "edu_institution" && state.me?.role === "curator");
}

function canPrepareCategory(category) {
  if (["super_admin", "holding_admin", "org_admin", "curator"].includes(state.me?.role)) return state.me?.entity_type !== "edu_institution";
  if (state.me?.role === "hr_specialist") return ["internship", "employment_practice"].includes(category);
  if (state.me?.role === "financial_specialist") return category === "teachers";
  return false;
}

function canCreateCategory(category) {
  if (["super_admin", "holding_admin", "org_admin", "curator"].includes(state.me?.role)) return state.me?.entity_type !== "edu_institution";
  return state.me?.role === "hr_specialist" && ["internship", "employment_practice"].includes(category) && state.me?.entity_type !== "edu_institution";
}

function canManageReportWorkflow() {
  return state.me?.entity_type !== "edu_institution" && ["super_admin", "holding_admin", "org_admin", "curator"].includes(state.me?.role);
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
    <div class="field"><label>Группа юридических лиц</label><select id="${prefix}-group-id"><option value="">Без договора о взаимодействии</option>${(state.legalEntityGroups || []).map((group) => `<option value="${group.id}" ${group.id === agreement.legal_entity_group_id ? "selected" : ""}>${escapeHTML(group.name)} · договор № ${escapeHTML(group.interaction_agreement_number)}</option>`).join("")}</select>${agreement.legal_entity_group && !agreement.legal_entity_group_id ? `<div class="field-hint">Legacy-значение: ${escapeHTML(agreement.legal_entity_group)}</div>` : ""}</div>
    <div class="field"><label>Способ подписания *</label><select id="${prefix}-signature"><option value="unsigned">Не подписано</option><option value="paper">Бумажный документ</option><option value="qualified_electronic">УКЭП</option><option value="goskey">Госключ</option></select></div>
    <div class="field"><label>Подписант (обязательно для действующего)</label><input id="${prefix}-signed-by" maxlength="300" value="${escapeHTML(agreement.signed_by || "")}"></div>
    <div class="field"><label>Дата подписания (обязательно для действующего)</label><input type="date" id="${prefix}-signature-date" value="${escapeHTML(agreement.signature_date || "")}"></div>
    <div class="field"><label>Ссылка / реквизиты документа</label><input id="${prefix}-document" maxlength="1000" value="${escapeHTML(agreement.document_reference || "")}"></div>
  </div>
  ${withPartners ? `<div class="field"><label>Учебные заведения, охваченные соглашением *</label><select id="${prefix}-partners" multiple size="5" required>${state.partners.map((partner) => `<option value="${partner.id}" data-kind="${partner.partner_kind}" ${(agreement.partner_ids || []).includes(partner.id) ? "selected" : ""}>${escapeHTML(partner.name)} (${escapeHTML(AUDIENCE_LABELS[partner.partner_kind])})</option>`).join("")}</select><div class="field-hint">Соглашение с РОИВ охватывает одну или несколько школ. Для вузов и СПО используется соглашение с образовательной организацией.</div></div>` : ""}
  <fieldset class="field" id="${prefix}-activities"><legend>Виды мероприятий, включённые в соглашение *</legend><div class="grid cols-2">${state.categories.map((category) => `<label class="check-row"><input type="checkbox" value="${escapeHTML(category.code)}" ${(agreement.activity_codes == null || agreement.activity_codes.includes(category.code)) ? "checked" : ""}> <span>${escapeHTML(category.name)}</span></label>`).join("")}</div><div class="field-hint">Готовность контролируется по каждому выбранному виду. Для нового соглашения выбраны все применимые виды; исключите только те, которые действительно не входят в согласованный перечень.</div></fieldset>
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
    const selectedKinds = partners
      ? [...partners.selectedOptions].map((option) => option.dataset.kind)
      : [kind.value === "roiv" ? "school" : "vuz", kind.value === "roiv" ? "school" : "kolledj"];
    const activityInputs = [...root.querySelectorAll(`#${prefix}-activities input[type=checkbox]`)];
    activityInputs.forEach((input) => {
      const category = state.categories.find((item) => item.code === input.value);
      const allowed = category && selectedKinds.some((selectedKind) => category.audience_scope.includes(selectedKind));
      input.disabled = !allowed;
      if (!allowed) input.checked = false;
    });
    const firstActivity = activityInputs.find((input) => !input.disabled);
    if (firstActivity) firstActivity.setCustomValidity(activityInputs.some((input) => !input.disabled && input.checked) ? "" : "Выберите хотя бы один вид мероприятия");
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
  root.querySelector(`#${prefix}-partners`)?.addEventListener("change", sync);
  root.querySelectorAll(`#${prefix}-activities input`).forEach((input) => input.addEventListener("change", sync));
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
    legal_entity_group: "",
    legal_entity_group_id: root.querySelector(`#${prefix}-group-id`).value,
    signature_method: root.querySelector(`#${prefix}-signature`).value,
    signed_by: root.querySelector(`#${prefix}-signed-by`).value.trim(),
    signature_date: root.querySelector(`#${prefix}-signature-date`).value,
    document_reference: root.querySelector(`#${prefix}-document`).value.trim(),
    notes: root.querySelector(`#${prefix}-notes`).value.trim(),
    activity_codes: [...root.querySelectorAll(`#${prefix}-activities input:checked:not(:disabled)`)].map((input) => input.value),
    responsible_people: [
      ...parseResponsiblePeople(root.querySelector(`#${prefix}-people-cp`).value, "cyberprotect"),
      ...parseResponsiblePeople(root.querySelector(`#${prefix}-people-other`).value, "counterparty"),
    ],
  };
}

function openUserProfile(user, refresh) {
  const modal = el(
    `<div class="modal-backdrop"><form class="modal" role="dialog" aria-modal="true"><h2>Доступ: ${escapeHTML(user.full_name)}</h2><div class="field"><label>Роль</label><select name="role"><option value="super_admin">SUPER_ADMIN</option><option value="holding_admin">HOLDING_ADMIN</option><option value="org_admin">ORG_ADMIN</option><option value="curator">CURATOR</option><option value="hr_specialist">HRD / HR_SPECIALIST</option><option value="financial_specialist">FINANCIAL_SPECIALIST</option><option value="legal_specialist">LEGAL_SPECIALIST</option><option value="auditor_viewer">AUDITOR_VIEWER</option></select></div><div class="field"><label>Тип пользователя</label><select name="entity"><option value="organization">ИТ-компания</option><option value="edu_institution">Учебное заведение</option></select></div><div class="field" data-education-assignment><label data-education-label>Учебное заведение *</label><select name="partner"><option value="">Выберите учебное заведение</option>${state.partners.map((p) => `<option value="${p.id}">${escapeHTML(p.name)}</option>`).join("")}</select></div><div class="field" data-company-assignment><label data-company-label>ИТ-компания</label><select name="company"><option value="">Выберите ИТ-компанию</option>${(state.itCompanies || []).map((company) => `<option value="${company.id}">${escapeHTML(company.name)} · ИНН ${escapeHTML(company.inn)}</option>`).join("")}</select></div><p class="error" role="alert"></p><button class="btn" type="submit">Сохранить доступ</button><button class="btn secondary" type="button">Закрыть</button></form></div>`,
  );
  const form = modal.querySelector("form");
  form.elements.role.value = user.role;
  form.elements.entity.value = user.entity_type || "edu_institution";
  form.elements.partner.value = user.partner_id || "";
  form.elements.company.value = user.it_company_id || "";
  const sync = () => {
    const education = form.elements.entity.value === "edu_institution";
    const curator = !education && form.elements.role.value === "curator";
    form.querySelector("[data-education-assignment]").hidden = !education && !curator;
    form.querySelector("[data-company-assignment]").hidden = education;
    form.elements.partner.disabled = !education && !curator;
    form.elements.company.disabled = education;
    form.querySelector("[data-education-label]").textContent = education ? "Учебное заведение *" : "Закреплённая ОО куратора";
    form.querySelector("[data-company-label]").textContent =
      `ИТ-компания${form.elements.role.value !== "super_admin" ? " *" : ""}`;
  };
  form.elements.role.onchange = sync;
  form.elements.entity.onchange = sync;
  sync();
  form.querySelector("[type=button]").onclick = () => modal.remove();
  form.onsubmit = async (e) => {
    e.preventDefault();
    const error = form.querySelector(".error");
    error.textContent = "";
    const education = form.elements.entity.value === "edu_institution";
    if (education && !form.elements.partner.value) {
      error.textContent = "Выберите учебное заведение";
      form.elements.partner.focus();
      return;
    }
    if (
      !education &&
      form.elements.role.value !== "super_admin" &&
      !form.elements.company.value
    ) {
      error.textContent = "Выберите ИТ-компанию";
      form.elements.company.focus();
      return;
    }
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
          it_company_id:
            form.elements.entity.value === "organization"
              ? form.elements.company.value
              : "",
        }),
      });
      modal.remove();
      await refresh();
      if (user.id === state.me.id) await boot();
    } catch (err) {
      error.textContent = err.message;
    } finally {
      b.disabled = false;
    }
  };
  document.body.appendChild(modal);
}
function validateFileBatch(files) {
  if (
    files.length > 20 ||
    [...files].reduce((sum, f) => sum + f.size, 0) > 20 * 1024 * 1024
  )
    throw new Error("Выберите до 20 файлов общим размером до 20 МБ");
  if ([...files].some((f) => !f.size || [...f.name].length > 255))
    throw new Error("Пустые файлы и имена длиннее 255 символов не допускаются");
}
const DOCUMENT_TYPE_LABELS = Object.freeze({
  other: "Прочий документ", employment_contract: "Трудовой договор / ГПХ",
  organization_agreement: "Договор с образовательной организацией", individual_plan: "Индивидуальный план",
  appointment_order: "Приказ о назначении / допуске", program_project: "Проект программы",
  expert_conclusion: "Экспертное заключение", academic_council_protocol: "Решение учёного совета",
  internship_agreement: "Договор о стажировке", practice_agreement: "Договор о практической подготовке",
  labor_contract: "Трудовой договор со студентом", mentor_order: "Приказ о наставнике",
  individual_program: "Индивидуальная программа / табель", incoming_certificate: "Входящая справка",
  outgoing_certificate: "Итоговая справка", top_agreement: "Договор ТОП-ИТ / ТОП-ИИ",
  payment_order: "Платёжное поручение", spending_act: "Акт фактического расходования",
  ano_letter: "Письмо АНО АЦ", school_agreement: "Соглашение со школой / РОИВ",
  participant_groups: "Реестр групп участников", acceptance_act: "Акт приёмки",
  digital_trace: "Цифровой след ФГИС «Моя школа»", ministry_decision: "Решение Минцифры и поручение",
  expense_evidence: "Первичные документы расходов", auditor_report: "Аудиторское заключение",
});

async function uploadFileBatch(id, files, documentType = "other") {
  validateFileBatch(files);
  if (!files.length) return;
  const body = new FormData();
  [...files].forEach((f) => body.append("files", f));
  await api(`/entries/${encodeURIComponent(id)}/attachments?${new URLSearchParams({ document_type: documentType })}`, {
    method: "POST",
    body,
  });
}
function workspaceQuery() {
  const query = new URLSearchParams({
    partner_id: state.partnerID,
    agreement_id: state.agreementID,
    category_code: state.categoryCode,
    period_type: state.period,
    report_year: state.year,
  });
  Object.entries(state.entryFilters || {}).forEach(([key, value]) => {
    if (String(value || "").trim()) query.set(key, String(value).trim());
  });
  return query;
}

function entryFiltersMarkup(category) {
  const value = (key) => escapeHTML(state.entryFilters?.[key] || "");
  const common = `<div class="field"><label>Поиск по реквизитам и ФИО</label><input data-entry-filter="q" value="${value("q")}" placeholder="Введите текст"></div><div class="field"><label>Метод стоимости</label><select data-entry-filter="cost_method"><option value="">Все методы</option><option value="average" ${value("cost_method") === "average" ? "selected" : ""}>Средние значения</option><option value="actual" ${value("cost_method") === "actual" ? "selected" : ""}>Фактические затраты</option></select></div>`;
  const byCategory = {
    teachers: `<div class="field"><label>ФИО преподавателя</label><input data-entry-filter="teacher_full_name" value="${value("teacher_full_name")}"></div><div class="field"><label>Направление подготовки</label><input data-entry-filter="training_direction" value="${value("training_direction")}"></div><div class="field"><label>Кафедра / институт / факультет</label><input data-entry-filter="structural_unit" value="${value("structural_unit")}"></div>`,
    ood_rpd: `<div class="field"><label>Вид документа</label><select data-entry-filter="doc_type"><option value="">Все</option><option value="rpd" ${value("doc_type") === "rpd" ? "selected" : ""}>РПД</option><option value="oop" ${value("doc_type") === "oop" ? "selected" : ""}>ООП</option></select></div><div class="field"><label>Вид активности</label><select data-entry-filter="activity_type"><option value="">Все</option><option value="development" ${value("activity_type") === "development" ? "selected" : ""}>Разработка</option><option value="update" ${value("activity_type") === "update" ? "selected" : ""}>Актуализация</option><option value="expertise" ${value("activity_type") === "expertise" ? "selected" : ""}>Экспертиза</option></select></div>`,
    internship: `<div class="field"><label>Продолжительность, мес.</label><input type="number" min="0" step="any" data-entry-filter="duration_months" value="${value("duration_months")}"></div><div class="field"><label>ФИО наставника</label><input data-entry-filter="mentor_name" value="${value("mentor_name")}"></div>`,
    employment_practice: `<div class="field"><label>Продолжительность, мес.</label><input type="number" min="0" step="any" data-entry-filter="duration_months" value="${value("duration_months")}"></div><div class="field"><label>ФИО наставника</label><input data-entry-filter="mentor_name" value="${value("mentor_name")}"></div>`,
    top_it: `<div class="field"><label>Тип активности</label><select data-entry-filter="activity_type"><option value="">Все</option><option value="assistance" ${value("activity_type") === "assistance" ? "selected" : ""}>Содействие</option><option value="cofinancing" ${value("activity_type") === "cofinancing" ? "selected" : ""}>Софинансирование</option></select></div><div class="field"><label>Продолжительность, мес.</label><input type="number" min="0" step="any" data-entry-filter="duration_months" value="${value("duration_months")}"></div><div class="field"><label>Тип затрат</label><input data-entry-filter="cost_type" value="${value("cost_type")}"></div>`,
  }[category] || "";
  return `<div class="grid cols-3">${common}${byCategory}</div><div class="flex"><button class="btn secondary" id="entry-filter-apply">Применить фильтры</button><button class="btn secondary" id="entry-filter-reset">Сбросить</button></div>`;
}
async function renderPartnerEntries(root, screen = CyberCalcScreens.activity(state.view)) {
  const generation = (root.workspaceGeneration || 0) + 1;
  root.workspaceGeneration = generation;
  if (!state.categories.length) {
    state.categories = await api("/categories");
    state.categories.forEach((c) => (CATEGORY_LABELS[c.code] = c.name));
  }
  const fixedEducationPartner = isEducationReviewer();
  if (fixedEducationPartner) state.partnerID = state.me.partner_id || "";
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
  const canPrepare = canPrepareCategory(state.categoryCode);
  const canCreate = canCreateCategory(state.categoryCode);
  const canManageWorkflow = canManageReportWorkflow();
  const writable = agreementIsUsable(selectedAgreement) && canPrepare;
  const screenCategories = screen?.categoryCodes || [];
  const available = state.categories.filter(
    (category) =>
      (!screenCategories.length || screenCategories.includes(category.code)) &&
      category.audience_scope.includes(partner?.partner_kind || state.partnerKind) &&
      (!selectedAgreement?.activity_codes || selectedAgreement.activity_codes.includes(category.code)),
  );
  if (!available.some((c) => c.code === state.categoryCode))
    state.categoryCode = available[0]?.code || "";
  const educationSelector = fixedEducationPartner
    ? `<div class="field"><label>Учебное заведение</label><input value="${escapeHTML(partner?.name || "Назначенное учебное заведение")}" readonly></div>`
    : `<div class="field"><label for="workspace-kind">1. Тип учебного заведения</label><select id="workspace-kind">${Object.entries(
        AUDIENCE_LABELS,
      )
        .map(
          ([k, v]) =>
            `<option value="${k}" ${k === state.partnerKind ? "selected" : ""}>${v}</option>`,
        )
        .join("")}</select></div>
    <div class="field"><label for="partner-search">Поиск учебного заведения</label><input id="partner-search" placeholder="Введите часть названия"></div>
    <div class="field"><label for="workspace-partner">2. Учебное заведение</label><select id="workspace-partner"></select></div>`;
  const headingTitle = screen?.title || (canPrepare ? "План и факт" : "Рассмотрение плана и отчёта");
  const headingText = screen?.subtitle || (canPrepare ? "" : "План и отчёт формирует ИТ-организация. Здесь образовательная организация просматривает направленный перечень и согласовывает факт либо возвращает замечания.");
  const screenFacts = screen?.facts?.length
    ? `<div class="activity-facts">${screen.facts.map((fact) => `<div><span class="activity-fact-mark"></span><b>${escapeHTML(fact)}</b></div>`).join("")}</div>`
    : "";
  root.innerHTML = `<section class="page-heading screen-heading"><div>${screen ? `<span class="eyebrow">Экран ${screen.number} · ${escapeHTML(screen.kind)}</span>` : ""}<h1>${escapeHTML(headingTitle)}</h1>${headingText ? `<p>${escapeHTML(headingText)}</p>` : ""}</div><span class="year-badge">${state.year}</span></section>
  ${screenFacts}
  <div class="card"><div class="grid cols-3">
    ${educationSelector}
    <div class="field"><label for="workspace-agreement">3. Соглашение</label><select id="workspace-agreement"><option value="">— Выберите —</option>${state.agreements.map((agreement) => `<option value="${agreement.id}" ${agreement.id === state.agreementID ? "selected" : ""}>${escapeHTML(agreementLabel(agreement))}</option>`).join("")}</select></div>
  </div><div class="flex workspace-actions">${canManageWorkflow && canReviewEducationDirectory() ? '<button class="btn secondary" id="open-directory">Справочник и соглашения</button>' : ""}${canManageWorkflow && isStaffUser() ? '<button class="btn secondary" id="edit-budget-target">Целевая сумма (3%)</button>' : ""}</div><p class="context-status">${selectedAgreement ? `${escapeHTML(AGREEMENT_KIND_LABELS[selectedAgreement.agreement_kind] || selectedAgreement.agreement_kind)} · ${canPrepare ? (writable ? "Доступно редактирование" : "Только просмотр: проверьте статус и срок соглашения") : "Режим рассмотрения образовательной организацией"}` : "Выберите соглашение"}</p></div>
  <div class="card"><div class="tabs"><button data-p="plan" class="${state.period === "plan" ? "active" : ""}">План</button><button data-p="fact" class="${state.period === "fact" ? "active" : ""}">Факт</button></div>
    <div class="grid cols-3"><div class="field"><label>Год</label><input type="number" id="year" min="2000" max="2100" step="1" value="${state.year}"></div>
    <div class="field"><label>${screen?.id === "schools" ? "Направление школьного трека" : "Категория активности"}</label><select id="category" ${available.length <= 1 ? "disabled" : ""}>${available.map((c) => `<option value="${c.code}" ${c.code === state.categoryCode ? "selected" : ""}>${escapeHTML(c.name)}</option>`).join("")}</select></div>
    <div class="field"><label>Режим</label>${canCreate ? `<button class="btn" id="add-entry" ${writable ? "" : "disabled"}>+ Добавить запись</button>` : `<input value="${canPrepare ? "Редактирование по роли" : "Просмотр и согласование"}" readonly>`}</div></div>
    <div class="flex">${screen?.id === "teachers" ? '<button class="btn secondary" id="staff-members">Сотрудники и ОКЗ</button><button class="btn secondary" id="teaching-payouts">График компенсаций</button>' : ""}${canManageWorkflow ? `<button class="btn secondary" id="import-entries" ${writable ? "" : "disabled"}>Импорт из Excel</button>` : ""}<a class="btn secondary" id="export-link">Excel: категория</a><a class="btn secondary" id="export-all-link">Excel: все активности учебного заведения</a><a class="btn secondary" id="export-word">Word: таблица</a></div>
  </div><div id="obligation-box"></div>
  <div class="card"><h2>Фильтры раздела</h2>${entryFiltersMarkup(state.categoryCode)}<div id="entries-summary"></div><div id="entries-table">${partner ? "Загрузка…" : "Выберите учебное заведение выше"}</div></div>`;
  const budgetTargetButton = root.querySelector("#edit-budget-target");
  if (budgetTargetButton)
    budgetTargetButton.onclick = () => openBudgetTargetDialog(budgetTargetButton);
  const partnerSelect = root.querySelector("#workspace-partner");
  if (partnerSelect) {
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
  }
  root.querySelector("#workspace-agreement").onchange = (e) => {
    state.agreementID = e.target.value;
    renderEntries(root);
  };
  const openDirectory = root.querySelector("#open-directory");
  if (openDirectory) openDirectory.onclick = () => {
    CyberCalcRouter.activate("partners");
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
    state.entryFilters = {};
    renderEntries(root);
  };
  root.querySelector("#entry-filter-apply").onclick = () => {
    state.entryFilters = {};
    root.querySelectorAll("[data-entry-filter]").forEach((input) => { if (input.value.trim()) state.entryFilters[input.dataset.entryFilter] = input.value.trim(); });
    state.entryPageOffset = 0;
    renderEntries(root);
  };
  root.querySelector("#entry-filter-reset").onclick = () => { state.entryFilters = {}; state.entryPageOffset = 0; renderEntries(root); };
  const addEntry = root.querySelector("#add-entry");
  if (addEntry) addEntry.onclick = () => openEntryModal(null);
  const importEntries = root.querySelector("#import-entries");
  if (importEntries) importEntries.onclick = () => openImportDialog(false);
  const staffMembers = root.querySelector("#staff-members");
  if (staffMembers) staffMembers.onclick = () => openStaffMembersDialog();
  const teachingPayouts = root.querySelector("#teaching-payouts");
  if (teachingPayouts) teachingPayouts.onclick = () => openTeachingPayoutsDialog();
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
  if (!state.agreementID) {
    root.querySelectorAll("a").forEach((a) => a.removeAttribute("href"));
    root.querySelector("#entries-table").textContent = "Добавьте и выберите соглашение.";
    return;
  }
  if (!available.length) {
    root.querySelectorAll("a").forEach((a) => a.removeAttribute("href"));
    root.querySelector("#obligation-box").innerHTML = '<div class="card notice">Выбранный экран не входит в перечень мероприятий соглашения или недоступен для этого типа образовательной организации.</div>';
    root.querySelector("#entries-table").textContent = "Нет доступных категорий для выбранного контекста.";
    return;
  }
  const chosen = state.partnerID;
  const [entries, workflow, summary] = await Promise.all([
    api(`/entries?${query}&offset=${state.entryPageOffset || 0}`),
    api(`/report-workflow?${new URLSearchParams({ agreement_id: state.agreementID, report_year: state.year, period_type: state.period })}`),
    api(`/entries/summary?${query}`),
  ]);
  if (
    chosen !== state.partnerID ||
    root.workspaceGeneration !== generation ||
    !root.querySelector("#entries-table")
  )
    return;
  state.entries = entries;
  const summaryBox = root.querySelector("#entries-summary");
  const categoryTotals = {
    teachers: `Партнёры: ${summary.partners_count} · курсы: ${summary.courses_count} · преподаватели: ${summary.teachers_count} · ак. часы: ${Number(summary.academic_hours || 0).toLocaleString("ru-RU")}`,
    ood_rpd: `Партнёры: ${summary.partners_count} · программы: ${summary.programs_count}`,
    internship: `Партнёры: ${summary.partners_count} · наставники: ${summary.mentors_count} · стажёры: ${summary.students_count}`,
    employment_practice: `Партнёры: ${summary.partners_count} · наставники: ${summary.mentors_count} · практиканты: ${summary.students_count}`,
  }[state.categoryCode] || `Партнёры: ${summary.partners_count}`;
  const matrix = summary.ood_rpd_matrix?.length ? `<details><summary>Выжимка ООП/РПД</summary><div class="table-wrap"><table><thead><tr><th>Документ</th><th>Активность</th><th>Количество</th><th>Сумма</th></tr></thead><tbody>${summary.ood_rpd_matrix.map((item) => `<tr><td>${escapeHTML(valueLabel(item.document_type))}</td><td>${escapeHTML(valueLabel(item.activity_type))}</td><td>${item.count}</td><td>${fmtMoney(item.amount_rub)}</td></tr>`).join("")}</tbody></table></div></details>` : "";
  const units = summary.structural_units?.length ? `<details><summary>Структурные подразделения</summary><div class="table-wrap"><table><thead><tr><th>Подразделение</th><th>Записи</th><th>Сумма</th></tr></thead><tbody>${summary.structural_units.map((item) => `<tr><td>${escapeHTML(item.unit)}</td><td>${item.count}</td><td>${fmtMoney(item.amount_rub)}</td></tr>`).join("")}</tbody></table></div></details>` : "";
  summaryBox.innerHTML = `<div class="grid cols-3"><div class="stat"><div class="label">Всего записей</div><div class="value">${Number(summary.count || 0).toLocaleString("ru-RU")}</div></div><div class="stat"><div class="label">Общая сумма</div><div class="value">${fmtMoney(summary.amount_rub)}</div></div><div class="stat"><div class="label">Итоговые показатели</div><div class="value" style="font-size:16px">${escapeHTML(categoryTotals)}</div></div></div>${matrix}${units}`;
  const obligation = root.querySelector("#obligation-box");
  const workflowLabels = { draft: "Черновик ИТ-организации", ready: state.period === "fact" ? "Направлено на рассмотрение" : "Готово", verified: state.period === "fact" ? "Согласовано ОО / РОИВ" : "Проверено", approved: "Утверждено" };
  const automaticOK = workflow.automatic_checks.every((check) => check.complete || check.code === "actual_costs");
  const confirmationsDisabled = workflow.status !== "draft" || !canManageWorkflow;
  const reviewHint = !canManageWorkflow && state.period === "fact"
    ? (workflow.status === "ready"
      ? (workflow.can_verify || workflow.can_return_draft
        ? '<p class="notice">Проверьте перечень. При согласии нажмите «Согласовать перечень»; при наличии замечаний верните его ИТ-организации с комментарием.</p>'
        : '<p class="notice">Соглашение охватывает несколько ОО. Итог рассмотрения фиксирует оператор, чтобы один представитель не согласовал данные за остальных.</p>')
      : '<p class="muted">Согласование станет доступно после направления перечня ИТ-организацией.</p>')
    : "";
  const workflowControls = canManageWorkflow
    ? `<div class="field"><label>Комментарий к смене статуса</label><textarea id="wf-comment" maxlength="1000" rows="2" placeholder="Основание проверки или возврата"></textarea></div>
      <div class="flex"><button class="btn" id="wf-ready" ${workflow.can_mark_ready ? "" : "disabled"}>${state.period === "fact" ? "Направить на рассмотрение" : "Передать: Готово"}</button><button class="btn" id="wf-verify" ${workflow.can_verify ? "" : "disabled"}>${state.period === "fact" ? "Зафиксировать согласование" : "Проверено"}</button><button class="btn" id="wf-approve" ${workflow.can_approve ? "" : "disabled"}>Утверждено</button><button class="btn secondary" id="wf-draft" ${workflow.can_return_draft ? "" : "disabled"}>Вернуть в черновик</button></div>`
    : state.period === "fact" && workflow.status === "ready" && (workflow.can_verify || workflow.can_return_draft)
      ? `<div class="field"><label>Комментарий к решению *</label><textarea id="wf-comment" maxlength="1000" rows="2" placeholder="Основание согласования или замечания к перечню"></textarea></div>
        <div class="flex"><button class="btn" id="wf-verify" ${workflow.can_verify ? "" : "disabled"}>Согласовать перечень</button><button class="btn secondary" id="wf-draft" ${workflow.can_return_draft ? "" : "disabled"}>Вернуть с замечаниями</button></div>`
      : '<p class="muted">Действий со стороны образовательной организации на этом этапе нет.</p>';
  obligation.innerHTML = `<div class="card"><div class="flex between"><div><h2>Комплектность отчёта по соглашению</h2><p>Статус: <span class="status-badge ${workflow.status === "approved" ? "active" : workflow.status === "draft" ? "inactive" : ""}">${escapeHTML(workflowLabels[workflow.status] || workflow.status)}</span></p></div><small>Контроль ведётся отдельно для ${state.period === "plan" ? "плана" : "факта"} ${state.year} года</small></div>
    ${workflow.review_due_date ? `<p class="${workflow.review_overdue ? "error" : "notice"}">${workflow.review_overdue ? "Просрочено рассмотрение" : "Срок рассмотрения"}: по ${escapeHTML(workflow.review_due_date.split("-").reverse().join("."))} включительно · ${Number(workflow.review_calendar_days)} календарных дней по приказу № 270.</p>` : ""}
    <h3>Автоматические проверки</h3><ul>${workflow.automatic_checks.map((check) => `<li>${check.complete ? "✓" : "✕"} ${escapeHTML(check.label)}</li>`).join("")}</ul>
    <h3>Виды мероприятий соглашения</h3><div class="grid cols-2">${workflow.activities.map((activity) => `<button class="btn secondary" data-required="${activity.code}">${activity.complete ? "✓" : "＋"} ${escapeHTML(activity.name)}</button>`).join("")}</div>
    ${workflow.top_it_exception ? '<p class="notice">Применено исключение ТОП ИТ/ИИ (п. 22 Порядка): обязательные Виды 1 и 3 реализованы в другой образовательной организации с утверждённым отчётом.</p>' : ""}
    <h3>Юридические подтверждения ИТ-организации</h3><label class="check-row"><input id="wf-scope" type="checkbox" ${workflow.scope_confirmed ? "checked" : ""} ${confirmationsDisabled ? "disabled" : ""}> Конкретный перечень, объём, сроки и условия соответствуют соглашению</label>
    <label class="check-row"><input id="wf-conditions" type="checkbox" ${workflow.conditions_confirmed ? "checked" : ""} ${confirmationsDisabled ? "disabled" : ""}> Выполнены условия реализации каждого вида из приложения № 1 приказа</label>
    <label class="check-row"><input id="wf-evidence" type="checkbox" ${workflow.evidence_confirmed ? "checked" : ""} ${confirmationsDisabled ? "disabled" : ""}> Подтверждающие документы имеются и позволяют установить факт мероприятия (загрузка в систему необязательна)</label>
    ${workflow.uses_actual_costs && state.period === "fact" ? `<div class="notice"><b>Используются фактические затраты</b><div class="field"><label>Реквизиты аудиторского заключения *</label><input id="wf-auditor-reference" maxlength="1000" value="${escapeHTML(workflow.auditor_report_reference || "")}" ${confirmationsDisabled ? "disabled" : ""}></div><label class="check-row"><input id="wf-actual-costs" type="checkbox" ${workflow.actual_costs_confirmed ? "checked" : ""} ${confirmationsDisabled ? "disabled" : ""}> Аудиторское заключение подтверждает достоверность фактических затрат</label></div>` : ""}
    ${state.period === "fact" ? `<p class="${workflow.counterparty_confirmed ? "notice" : "muted"}">${workflow.counterparty_confirmed ? "✓ Перечень рассмотрен и согласован образовательной организацией / РОИВ" : "Рассмотрение образовательной организацией / РОИВ ещё не зафиксировано"}</p>` : ""}
    ${reviewHint}
    ${workflowControls}
    ${workflow.missing.length ? `<p class="error">Не выполнено: ${workflow.missing.map(escapeHTML).join("; ")}</p>` : ""}
    ${workflow.history.length ? `<details><summary>История согласования (${workflow.history.length})</summary><ul>${workflow.history.map((item) => `<li>${new Date(item.changed_at).toLocaleString("ru-RU")} · ${escapeHTML(item.changed_by)}: ${escapeHTML(workflowLabels[item.from_status] || item.from_status)} → ${escapeHTML(workflowLabels[item.to_status] || item.to_status)} — ${escapeHTML(item.comment)}</li>`).join("")}</ul></details>` : ""}<p class="muted">Любое изменение соглашения, перечня или записи автоматически возвращает этот отчёт в черновик. Экспорт разрешён только после утверждения.</p></div>`;
  obligation.querySelectorAll("[data-required]").forEach((button) => button.onclick = () => { state.categoryCode = button.dataset.required; renderEntries(root); });
  const transition = async (nextStatus) => {
    const button = obligation.querySelector(`#wf-${nextStatus === "ready" ? "ready" : nextStatus === "verified" ? "verify" : nextStatus === "approved" ? "approve" : "draft"}`);
    button.disabled = true;
    try {
      await api(`/report-workflow/transition?${new URLSearchParams({ agreement_id: state.agreementID, report_year: state.year, period_type: state.period })}`, { method: "POST", body: JSON.stringify({ status: nextStatus, scope_confirmed: obligation.querySelector("#wf-scope")?.checked || false, conditions_confirmed: obligation.querySelector("#wf-conditions")?.checked || false, evidence_confirmed: obligation.querySelector("#wf-evidence")?.checked || false, actual_costs_confirmed: obligation.querySelector("#wf-actual-costs")?.checked || false, auditor_report_reference: obligation.querySelector("#wf-auditor-reference")?.value.trim() || "", counterparty_confirmed: obligation.querySelector("#wf-counterparty")?.checked || false, comment: obligation.querySelector("#wf-comment").value.trim() }) });
      showToast(`Статус: ${workflowLabels[nextStatus]}`, "success");
      await renderEntries(root);
    } catch (error) { showToast(error.message); button.disabled = false; }
  };
  const readyButton = obligation.querySelector("#wf-ready");
  const verifyButton = obligation.querySelector("#wf-verify");
  const approveButton = obligation.querySelector("#wf-approve");
  const draftButton = obligation.querySelector("#wf-draft");
  if (readyButton) readyButton.onclick = () => transition("ready");
  if (verifyButton) verifyButton.onclick = () => transition("verified");
  if (approveButton) approveButton.onclick = () => transition("approved");
  if (draftButton) draftButton.onclick = () => transition("draft");
  const syncReady = () => {
    if (!readyButton) return;
    const actualOK = !workflow.uses_actual_costs || state.period !== "fact" || (obligation.querySelector("#wf-actual-costs")?.checked && obligation.querySelector("#wf-auditor-reference")?.value.trim());
    const manualOK = obligation.querySelector("#wf-scope")?.checked && obligation.querySelector("#wf-conditions")?.checked && obligation.querySelector("#wf-evidence")?.checked && actualOK;
    readyButton.disabled = !(canManageWorkflow && workflow.status === "draft" && automaticOK && manualOK);
  };
  obligation.querySelectorAll("input[type=checkbox]").forEach((input) => input.addEventListener("change", syncReady));
  obligation.querySelector("#wf-auditor-reference")?.addEventListener("input", syncReady);
  syncReady();
  if (workflow.status !== "approved") {
    ["#export-link", "#export-all-link", "#export-word"].forEach((selector) => { const link = root.querySelector(selector); link.removeAttribute("href"); link.classList.add("disabled"); link.title = "Экспорт откроется после утверждения отчёта"; });
  }
  const paint = () => {
    const list = entries;
    const fields =
      currentCategory()?.fields.filter(
        (f) => !["org_name", "mentor_id", "staff_member_id"].includes(f.key),
      ) || [];
    root.querySelector("#entries-table").innerHTML =
      `<p>На странице: ${list.length}. Итоги выше рассчитаны по всей выборке.</p><div class="table-wrap"><table><thead><tr>${fields.map((f) => `<th>${escapeHTML(f.label)}</th>`).join("")}<th>Готовность</th><th>Метод</th><th>Затраты</th><th></th></tr></thead><tbody>${list.map((e) => `<tr>${fields.map((f) => `<td>${escapeHTML(f.type === "select" ? valueLabel(e.payload[f.key] ?? "—") : e.payload[f.key] ?? "—")}</td>`).join("")}<td>${CyberCalcUI.riskBadge({ state: e.compliance?.state || "red", label: e.compliance?.state === "green" ? "Готово" : e.compliance?.state === "yellow" ? "Доработать" : "Риск", reasons: [...(e.compliance?.blocking_reasons || []), ...(e.compliance?.warnings || [])] })}</td><td>${e.cost_method === "actual" ? "Фактические" : "Средние"}</td><td>${fmtMoney(e.amount_rub)}</td><td><button class="btn secondary" data-edit="${e.id}">${writable ? "Открыть" : "Просмотреть"}</button></td></tr>`).join("")}</tbody></table></div>${!list.length ? `<p class="muted">${canCreate ? "Записей нет. Добавьте запись вручную." : "ИТ-организация ещё не добавила записи в этот раздел."}</p>` : ""}`;
    root
      .querySelectorAll("[data-edit]")
      .forEach(
        (b) =>
          (b.onclick = () =>
            openEntryModal(entries.find((e) => e.id === b.dataset.edit), !writable)),
      );
  };
  paint();
  const paging = el(`<div class="actions"><button class="btn secondary" id="entries-prev">Назад</button><span>Страница ${Math.floor((state.entryPageOffset || 0) / 200) + 1}</span><button class="btn secondary" id="entries-next">Далее</button></div>`);
  root.querySelector("#entries-table").after(paging);
  paging.querySelector("#entries-prev").disabled = !state.entryPageOffset;
  paging.querySelector("#entries-next").disabled = entries.nextOffset == null;
  paging.querySelector("#entries-prev").onclick = () => { state.entryPageOffset = Math.max(0, state.entryPageOffset - 200); renderEntries(root); };
  paging.querySelector("#entries-next").onclick = () => { state.entryPageOffset = entries.nextOffset; renderEntries(root); };
}

async function openImportDialog(directory, onCommitted = null) {
  const query = workspaceQuery();
  const endpoint = directory
    ? "/admin/directory-import"
    : `/entries/import?${query}`;
  const template = directory
    ? "/api/admin/directory-template"
    : `/api/entries/import-template?${query}`;
  const modal = el(
    `<div class="modal-backdrop"><div class="modal" role="dialog" aria-modal="true"><h2>Импорт ${directory ? "справочника" : "записей"} из Excel</h2><p>Первый лист .xlsx, до 8 МБ и ${directory ? "10000" : "1000"} строк. Сначала скачайте шаблон. Формулы замените значениями. ${directory ? "Выгрузка содержит текущий справочник. Исправьте нужные строки и укажите «Подтвердить» в столбце «Действие»; пустые строки этого столбца останутся без изменений. Для подтверждения обязательны ИНН, ОГРН, дата и HTTPS-ссылка официального источника." : "Наставник должен заранее присутствовать в справочнике выбранного партнёра. Суммы рассчитывает сервер."}</p><a class="btn secondary" href="${template}">Скачать текущий справочник</a><div class="field"><input type="file" accept=".xlsx" id="import-file" aria-label="Файл Excel"></div><div id="import-result" role="status"></div><div class="flex"><button class="btn" id="preview">Проверить</button><button class="btn" id="commit" disabled>${directory ? "Импортировать и подтвердить" : "Импортировать"}</button><button class="btn secondary" id="close-import">Закрыть</button></div></div></div>`,
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
        else if (onCommitted) await onCommitted();
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

async function renderITCompanies(root, embedded = false) {
  if (!canViewITCompanies()) {
    root.innerHTML = '<div class="card error-state">Реестр ИТ-компаний недоступен для этого профиля</div>';
    return;
  }
  const writable = canManageITCompanies();
  root.innerHTML = `${embedded ? '<div class="section-intro"><h2>Аккредитованные ИТ-компании</h2></div>' : '<section class="page-heading"><div><h1>Аккредитованные ИТ-компании</h1></div></section>'}
  <div class="card filter-card"><div class="flex between"><h2>Поиск по реестру</h2><div class="flex"><a class="btn secondary" href="https://www.gosuslugi.ru/itorgs" target="_blank" rel="noopener noreferrer">Открыть Госуслуги</a>${writable ? '<button class="btn" id="it-add">+ Добавить компанию</button>' : ""}</div></div><div class="directory-search"><div class="field"><label for="it-scope">Источник</label><select id="it-scope"><option value="all">Все источники</option><option value="saved">Сохранённые компании</option><option value="registry">Госуслуги через ПроРеестр</option></select></div><div class="field"><label for="it-search">Название или ИНН</label><input id="it-search" placeholder="Введите название или ИНН"></div><button class="btn secondary" id="it-find">Найти</button></div></div>
  <div class="card"><div class="flex between"><h2>Компании с действующей аккредитацией</h2><span class="count-badge" id="it-count"></span></div><p class="muted" id="it-scope-note"></p><div id="it-list" class="loading-state"><span class="spinner"></span>Загрузка реестра…</div></div>`;

  let offset = 0;
  let generation = 0;
  const pagination = el('<div class="flex"><button class="btn secondary" data-prev disabled>Предыдущая страница</button><button class="btn secondary" data-next disabled>Следующая страница</button></div>');
  root.querySelector("#it-list").after(pagination);
  const load = async () => {
    const version = ++generation;
    const list = root.querySelector("#it-list");
    list.className = "loading-state";
    list.innerHTML = '<span class="spinner"></span>Загрузка реестра…';
    try {
      const scope = root.querySelector("#it-scope").value;
      const registryOnly = scope === "registry";
      const searchText = root.querySelector("#it-search").value.trim();
      if (registryOnly && !searchText) {
        root.querySelector("#it-count").textContent = "";
        pagination.style.display = "none";
        root.querySelector("#it-scope-note").textContent = "Для проверки в реестре введите название или ИНН компании.";
        list.className = "empty-state";
        list.textContent = "Введите название или ИНН и нажмите «Найти».";
        return;
      }
      const query = encodeURIComponent(searchText);
      const combined = scope === "all" && searchText !== "";
      let companies = [];
      let nextOffset = null;
      let registryResult = null;
      let registryError = null;
      if (combined) {
        const [saved, external] = await Promise.all([
          api(`/it-companies?q=${query}&offset=0`),
          api(`/it-companies/registry-search?q=${query}`).catch((error) => ({ error })),
        ]);
        registryError = external.error || null;
        registryResult = registryError ? null : external;
        const seen = new Set();
        companies = saved.map((company) => {
          seen.add(company.inn);
          return { ...company, lookup_source: "saved" };
        });
        for (const company of registryResult?.items || []) {
          if (!seen.has(company.inn)) {
            seen.add(company.inn);
            companies.push({ ...company, lookup_source: "registry" });
          }
        }
      } else if (registryOnly) {
        registryResult = await api(`/it-companies/registry-search?q=${query}`);
        companies = registryResult.items.map((company) => ({ ...company, lookup_source: "registry" }));
      } else {
        const saved = await api(`/it-companies?q=${query}&offset=${offset}`);
        nextOffset = saved.nextOffset;
        companies = saved.map((company) => ({ ...company, lookup_source: "saved" }));
      }
      if (version !== generation) return;
      const localListing = !registryOnly && !combined;
      root.querySelector("#it-count").textContent = companies.length ? (localListing ? `Записи ${offset + 1}–${offset + companies.length}` : `Найдено: ${companies.length}`) : "0 компаний";
      pagination.style.display = localListing ? "" : "none";
      root.querySelector("#it-scope-note").textContent = combined
        ? registryError
          ? `Показаны совпадения из сохранённых компаний. Внешний реестр временно недоступен: ${registryError.message}`
          : registryResult.may_have_more
            ? "Совпадения из сохранённых компаний и первые 100 результатов внешнего реестра. Уточните запрос."
            : "Поиск выполнен по сохранённым компаниям и внешнему реестру."
        : registryOnly
          ? (registryResult.initial ? "Введите название или ИНН для поиска нужной организации." : registryResult.may_have_more ? "Показаны первые 100 совпадений. Уточните название или введите ИНН." : "Результат проверки реестра через ПроРеестр. Для подтверждающих документов откройте Госуслуги.")
          : scope === "all"
            ? "Показаны сохранённые компании. Введите название или ИНН, чтобы также проверить внешний реестр."
            : "Записи, добавленные вручную или загруженные из выгрузки.";
      pagination.querySelector("[data-prev]").disabled = offset === 0;
      pagination.querySelector("[data-next]").disabled = nextOffset == null;
      pagination.querySelector("[data-next]").onclick = () => { offset = nextOffset; load(); };
      list.className = "";
      list.innerHTML = companies.length
        ? `<div class="table-wrap"><table><thead><tr><th>Компания</th><th>Реквизиты</th><th>Аккредитация</th><th>Источник</th></tr></thead><tbody>${companies.map((company) => { const fromRegistry = company.lookup_source === "registry"; return `<tr><td><b>${escapeHTML(company.name)}</b>${company.director_name ? `<br><small>Руководитель: ${escapeHTML(company.director_name)}</small>` : ""}${company.legal_address ? `<br><small>${escapeHTML(company.legal_address)}</small>` : ""}${!fromRegistry && company.notes ? `<br><small>${escapeHTML(company.notes)}</small>` : ""}</td><td>ИНН ${escapeHTML(company.inn)}<br>ОГРН ${escapeHTML(company.ogrn || "не предоставлен источником")}${company.phone ? `<br>${escapeHTML(company.phone)}` : ""}${company.email ? `<br>${escapeHTML(company.email)}` : ""}</td><td><span class="status-badge active">Действует</span>${company.accreditation_number ? `<br>№ ${escapeHTML(company.accreditation_number)}` : ""}<br><small>${fromRegistry ? "Запрос выполнен" : "Данные на"} ${new Date(`${company.registry_updated_at}T00:00:00`).toLocaleDateString("ru-RU")}</small></td><td><span class="status-badge ${fromRegistry ? "pending" : "active"}">${fromRegistry ? "Внешний реестр" : "Сохранено"}</span><br><a href="${escapeHTML(company.source_url)}" target="_blank" rel="noopener noreferrer">Открыть источник сведений</a>${company.website ? `<br><a href="${escapeHTML(company.website)}" target="_blank" rel="noopener noreferrer">Сайт компании</a>` : ""}</td></tr>`; }).join("")}</tbody></table></div>`
        : `<div class="empty-state"><b>Компании не найдены</b><span>${registryError ? "В сохранённых данных совпадений нет, а внешний реестр временно недоступен. Повторите поиск позже." : "Проверьте ИНН или введите полное название юридического лица."}</span></div>`;
    } catch (error) {
      if (version !== generation) return;
      list.className = "error-state";
      list.innerHTML = `<span>${escapeHTML(error.message)}</span><button type="button" class="btn secondary" data-it-retry>Повторить</button><button type="button" class="btn secondary" data-it-saved>Открыть сохранённые компании</button>`;
      list.querySelector("[data-it-retry]").onclick = load;
      list.querySelector("[data-it-saved]").onclick = () => {
        root.querySelector("#it-scope").value = "saved";
        root.querySelector("#it-search").value = "";
        offset = 0;
        load();
      };
    }
  };
  const search = () => { offset = 0; load(); };
  pagination.querySelector("[data-prev]").onclick = () => { offset = Math.max(0, offset - 500); load(); };
  root.querySelector("#it-find").onclick = search;
  root.querySelector("#it-scope").onchange = search;
  if (writable) {
    const importButton = el('<button class="btn secondary" type="button">Загрузить выгрузку</button>');
    root.querySelector("#it-add").before(importButton);
    importButton.onclick = () => {
      const modal = el(`<div class="modal-backdrop"><form class="modal" role="dialog" aria-modal="true"><h2>Загрузить ИТ-компании</h2><p>CSV в UTF-8 или XLSX, до 100 000 строк. Для действующих аккредитаций нужны сведения не старше 35 дней. Существующие записи обновятся по ИНН.</p><p><a href="/api/it-companies/template" target="_blank" rel="noopener">Скачать шаблон</a></p><div class="field"><label>Выгрузка реестра<input type="file" name="file" accept=".csv,.xlsx" required></label></div><p class="notice" data-preview hidden></p><p class="error" role="alert"></p><div class="flex"><button class="btn" type="submit">Проверить файл</button><button class="btn secondary" type="button" data-close>Отмена</button></div></form></div>`);
      document.body.appendChild(modal);
      const form = modal.querySelector("form");
      let checked = false;
      form.querySelector("[data-close]").onclick = () => modal.remove();
      form.querySelector('[type="file"]').onchange = () => { checked = false; form.querySelector('[type="submit"]').textContent = "Проверить файл"; form.querySelector("[data-preview]").hidden = true; };
      form.onsubmit = async (event) => {
        event.preventDefault();
        const button = form.querySelector('[type="submit"]');
        button.disabled = true;
        form.querySelector(".error").textContent = "";
        try {
          const result = await api(`/it-companies/import${checked ? "?commit=1" : ""}`, {method: "POST", body: new FormData(form)});
          if (result.committed) { modal.remove(); await load(); showToast(`Загружено компаний: ${result.valid_rows}`); }
          else { checked = true; button.textContent = "Загрузить в справочник"; const preview = form.querySelector("[data-preview]"); preview.hidden = false; preview.textContent = `Проверено компаний: ${result.valid_rows}. Файл готов к загрузке.`; }
        } catch (error) { form.querySelector(".error").textContent = error.message; }
        finally { button.disabled = false; }
      };
    };
  }
  root.querySelector("#it-search").onkeydown = (event) => {
    if (event.key === "Enter") search();
  };
  if (writable) root.querySelector("#it-add").onclick = () => {
    const today = new Date().toISOString().slice(0, 10);
    const modal = el(`<div class="modal-backdrop"><form class="modal" role="dialog" aria-modal="true"><span class="eyebrow">Новая запись</span><h2>Добавить аккредитованную ИТ-компанию</h2><p class="notice">Сначала <a href="https://www.gosuslugi.ru/itorgs" target="_blank" rel="noopener noreferrer">проверьте аккредитацию на Госуслугах</a>, затем перенесите реквизиты без изменений. ИНН и ОГРН будут проверены.</p><div class="field"><label>Полное наименование *</label><input name="name" required minlength="2" maxlength="1000"></div><div class="grid cols-2"><div class="field"><label>ИНН *</label><input name="inn" required inputmode="numeric" pattern="[0-9]{10}|[0-9]{12}"></div><div class="field"><label>ОГРН / ОГРНИП *</label><input name="ogrn" required inputmode="numeric" pattern="[0-9]{13}|[0-9]{15}"></div><div class="field"><label>Номер аккредитации, если указан</label><input name="accreditation_number" maxlength="100"></div><div class="field"><label>Дата проверки в реестре *</label><input name="registry_updated_at" type="date" max="${today}" value="${today}" required></div></div><div class="field"><label>Адрес в пределах места нахождения</label><input name="legal_address" maxlength="1000"></div><div class="grid cols-2"><div class="field"><label>Руководитель</label><input name="director_name" maxlength="300"></div><div class="field"><label>Телефон</label><input name="phone" maxlength="100"></div><div class="field"><label>Email</label><input name="email" type="email" maxlength="254"></div><div class="field"><label>Сайт</label><input name="website" type="url" placeholder="https://" maxlength="1000"></div></div><div class="field"><label>Ссылка на официальный источник *</label><input name="source_url" type="url" required value="https://www.gosuslugi.ru/itorgs"></div><div class="field"><label>Примечание</label><textarea name="notes" maxlength="1000" rows="2"></textarea></div><p class="error" role="alert"></p><div class="flex"><button class="btn" type="submit">Добавить компанию</button><button class="btn secondary" type="button">Отмена</button></div></form></div>`);
    const form = modal.querySelector("form");
    form.querySelector('[type="button"]').onclick = () => modal.remove();
    form.onsubmit = async (event) => {
      event.preventDefault();
      if (!form.reportValidity()) return;
      const button = form.querySelector('[type="submit"]');
      button.disabled = true;
      button.textContent = "Добавляем…";
      try {
        await api("/it-companies", { method: "POST", body: JSON.stringify(Object.fromEntries(new FormData(form))) });
        modal.remove();
        await load();
        showToast("ИТ-компания добавлена", "success");
      } catch (error) {
        form.querySelector(".error").textContent = error.message;
        button.disabled = false;
        button.textContent = "Добавить компанию";
      }
    };
    document.body.appendChild(modal);
  };
  await load();
}

function directoryStatusLabel(value) {
  return {
    active: "Действует",
    suspended: "Приостановлена",
    expired: "Истекла",
    revoked: "Аннулирована",
    unknown: "Не указан",
    inactive: "Не действует",
    reorganized: "Реорганизована",
    liquidated: "Ликвидирована",
  }[value] || value || "Не указан";
}

async function openDirectoryReview(item, onSaved = async () => {}) {
  if (!item || !canApproveEducationDirectory()) return;
  const today = new Date().toISOString().slice(0, 10);
  const option = (value, label, selected) =>
    `<option value="${value}" ${value === selected ? "selected" : ""}>${label}</option>`;
  const modal = el(`<div class="modal-backdrop"><form class="modal modal-wide" role="dialog" aria-modal="true">
    <div class="flex between"><div><span class="eyebrow">Проверка справочника</span><h2>${escapeHTML(item.name)}</h2></div><button class="btn secondary" type="button" data-close>Закрыть</button></div>
    <p class="notice">Автоматически найденные реквизиты могут относиться к одноимённой организации или головному юридическому лицу. Сверьте ИНН и ОГРН с официальным источником.</p>
    <div class="grid cols-2">
      <div class="field"><label>Полное наименование *</label><input name="name" required minlength="2" maxlength="1000" value="${escapeHTML(item.name || "")}"></div>
      <div class="field"><label>Тип учебного заведения *</label><select name="partner_kind">${option("vuz", "Вуз", item.partner_kind)}${option("kolledj", "Колледж / СПО", item.partner_kind)}${option("school", "Школа", item.partner_kind)}</select></div>
      <div class="field"><label>Регион *</label><input name="region" required minlength="2" maxlength="200" value="${escapeHTML(item.region || "")}"></div>
      <div class="field"><label>Дата актуальности *</label><input name="registry_updated_at" type="date" required max="${today}" value="${escapeHTML(item.registry_updated_at || today)}"></div>
      <div class="field"><label>ИНН *</label><input name="inn" inputmode="numeric" pattern="[0-9]{10}|[0-9]{12}" required value="${escapeHTML(item.inn || "")}"></div>
      <div class="field"><label>ОГРН / ОГРНИП *</label><input name="ogrn" inputmode="numeric" pattern="[0-9]{13}|[0-9]{15}" required value="${escapeHTML(item.ogrn || "")}"></div>
      <div class="field"><label>Номер лицензии</label><input name="license_number" maxlength="100" value="${escapeHTML(item.license_number || "")}"></div>
      <div class="field"><label>Статус лицензии</label><select name="license_status">${option("unknown", "Не указан", item.license_status)}${option("active", "Действует", item.license_status)}${option("suspended", "Приостановлена", item.license_status)}${option("expired", "Истекла", item.license_status)}${option("revoked", "Аннулирована", item.license_status)}</select></div>
      <div class="field"><label>Статус организации</label><select name="institution_status">${option("unknown", "Не указан", item.institution_status)}${option("active", "Действует", item.institution_status)}${option("inactive", "Не действует", item.institution_status)}${option("reorganized", "Реорганизована", item.institution_status)}${option("liquidated", "Ликвидирована", item.institution_status)}</select></div>
      <div class="field"><label>Официальный источник *</label><input name="source_url" type="url" required value="${escapeHTML(item.source_url || "https://egrul.nalog.ru/index.html")}"></div>
      <div class="field"><label>Коды направлений</label><input name="program_codes" value="${escapeHTML((item.program_codes || []).join(", "))}" placeholder="09.03.01, 38.03.05"><small>Для вуза нужен хотя бы один точный код из приказа Минцифры № 27.</small></div>
      <div class="field"><label>Источник направлений</label><input name="programs_source_url" type="url" value="${escapeHTML(item.programs_source_url || "")}" placeholder="https://сайт-вуза/sveden/education/"></div>
    </div>
    <div class="field"><label>Комментарий к проверке</label><textarea name="confirmation_comment" maxlength="1000" rows="2" placeholder="Что именно проверено или исправлено"></textarea></div>
    <p class="muted">«Сохранить изменения» оставит жёлтый статус. «Подтвердить» зафиксирует ваши ФИО, email, время и полный состав реквизитов в журнале изменений.</p>
    <p class="error" role="alert"></p>
    <div class="flex"><button class="btn secondary" type="submit" data-mode="save">Сохранить изменения</button><button class="btn" type="submit" data-mode="confirm">Подтвердить</button></div>
  </form></div>`);
  document.body.appendChild(modal);
  const form = modal.querySelector("form");
  modal.querySelector("[data-close]").onclick = () => modal.remove();
  form.onsubmit = async (event) => {
    event.preventDefault();
    if (!form.reportValidity()) return;
    const confirm = event.submitter?.dataset.mode === "confirm";
    const buttons = form.querySelectorAll("button");
    buttons.forEach((button) => (button.disabled = true));
    form.querySelector(".error").textContent = "";
    try {
      const payload = Object.fromEntries(new FormData(form));
      payload.confirm = confirm;
      payload.program_codes = payload.program_codes.split(/[,;\s]+/).filter(Boolean);
      await api(`/directory/${encodeURIComponent(item.id)}`, {
        method: "PUT",
        body: JSON.stringify(payload),
      });
      modal.remove();
      await onSaved();
      showToast(confirm ? "Учебное заведение подтверждено" : "Изменения сохранены; требуется подтверждение", "success");
    } catch (error) {
      form.querySelector(".error").textContent = error.message;
      buttons.forEach((button) => (button.disabled = false));
    }
  };
}

async function openLegalEntityGroups(onSaved) {
  const modal = el(`<div class="modal-backdrop"><div class="modal modal-wide" role="dialog" aria-modal="true"><div class="flex between"><h2>Группа юридических лиц</h2><button class="btn secondary" id="group-close">Закрыть</button></div><div id="group-list"></div><form id="group-form"><h3 id="group-title">Договор о взаимодействии</h3><div class="grid cols-2"><div class="field"><label>Название группы *</label><input name="name" required maxlength="500"></div><div class="field"><label>Номер договора о взаимодействии *</label><input name="agreement_number" required maxlength="100"></div><div class="field"><label>Дата договора *</label><input name="agreement_date" type="date" required></div><div class="field"><label>Уполномоченное юридическое лицо *</label><input name="authorized_name" required maxlength="1000"></div><div class="field"><label>ИНН уполномоченного лица *</label><input name="authorized_inn" required inputmode="numeric" pattern="[0-9]{10}|[0-9]{12}" maxlength="12"></div><div class="field"><label>ОГРН / ОГРНИП *</label><input name="authorized_ogrn" required inputmode="numeric" pattern="[0-9]{13}|[0-9]{15}" maxlength="15"></div></div><div class="field"><label>Участники группы *</label><textarea name="members" rows="6" required placeholder="Название | ИНН | ОГРН | ИТ или иное | целевой объём, руб."></textarea><div class="field-hint">Один участник на строку. Уполномоченное лицо тоже добавьте в список. Пример: ООО Компания | 7700000000 | 1027700000000 | ИТ | 1500000</div></div><div class="flex"><button class="btn" type="submit">Сохранить</button></div><p class="error" role="alert"></p></form></div></div>`);
  const form = modal.querySelector("#group-form");
  let editingID = "";
  const memberText = (members) => (members || []).map((member) => [member.name, member.inn, member.ogrn, member.is_it_organization ? "ИТ" : "иное", member.target_amount_rub || ""].join(" | ")).join("\n");
  const fill = (group = {}) => {
    editingID = group.id || "";
    modal.querySelector("#group-title").textContent = editingID ? "Договор о взаимодействии" : "Новый договор о взаимодействии";
    form.elements.name.value = group.name || "";
    form.elements.agreement_number.value = group.interaction_agreement_number || "";
    form.elements.agreement_date.value = group.interaction_agreement_date || "";
    form.elements.authorized_name.value = group.authorized_entity_name || "";
    form.elements.authorized_inn.value = group.authorized_entity_inn || "";
    form.elements.authorized_ogrn.value = group.authorized_entity_ogrn || "";
    form.elements.members.value = memberText(group.members);
  };
  const load = async () => {
    state.legalEntityGroups = await api("/legal-entity-groups");
    modal.querySelector("#group-list").innerHTML = state.legalEntityGroups.length ? `<div class="table-wrap"><table><thead><tr><th>Группа и договор</th><th>Уполномоченное лицо</th><th>Участники</th><th></th></tr></thead><tbody>${state.legalEntityGroups.map((group) => `<tr><td><b>${escapeHTML(group.name)}</b><br>№ ${escapeHTML(group.interaction_agreement_number)} от ${escapeHTML(group.interaction_agreement_date)}</td><td>${escapeHTML(group.authorized_entity_name)}<br>ИНН ${escapeHTML(group.authorized_entity_inn)}</td><td>${group.members.length}</td><td><button class="btn secondary" data-group-edit="${group.id}">Изменить</button></td></tr>`).join("")}</tbody></table></div>` : '<p class="muted">Договор о взаимодействии ещё не добавлен.</p>';
    modal.querySelectorAll("[data-group-edit]").forEach((button) => button.onclick = () => fill(state.legalEntityGroups.find((group) => group.id === button.dataset.groupEdit)));
    if (state.legalEntityGroups.length === 1 && !editingID) fill(state.legalEntityGroups[0]);
  };
  modal.querySelector("#group-close").onclick = () => modal.remove();
  form.onsubmit = async (event) => {
    event.preventDefault();
    const error = form.querySelector('[role="alert"]');
    error.textContent = "";
    const members = form.elements.members.value.split("\n").map((line) => line.trim()).filter(Boolean).map((line) => {
      const [name = "", inn = "", ogrn = "", kind = "", target = ""] = line.split("|").map((value) => value.trim());
      return { name, inn, ogrn, is_it_organization: kind.toLocaleLowerCase("ru").startsWith("ит"), target_amount_rub: target ? Number(target.replace(",", ".")) : null };
    });
    if (!members.length || members.some((member) => !member.name || !member.inn || !member.ogrn || (member.target_amount_rub != null && (!Number.isFinite(member.target_amount_rub) || member.target_amount_rub <= 0)))) {
      error.textContent = "Проверьте строки участников: название, ИНН и ОГРН обязательны, целевой объём должен быть положительным.";
      return;
    }
    if (!members.some((member) => member.inn === form.elements.authorized_inn.value.trim())) {
      error.textContent = "Добавьте уполномоченное юридическое лицо в список участников с тем же ИНН.";
      return;
    }
    const button = form.querySelector('[type="submit"]'); button.disabled = true;
    try {
      await api(editingID ? `/legal-entity-groups/${editingID}` : "/legal-entity-groups", { method: editingID ? "PUT" : "POST", body: JSON.stringify({ name: form.elements.name.value.trim(), interaction_agreement_number: form.elements.agreement_number.value.trim(), interaction_agreement_date: form.elements.agreement_date.value, authorized_entity_name: form.elements.authorized_name.value.trim(), authorized_entity_inn: form.elements.authorized_inn.value.trim(), authorized_entity_ogrn: form.elements.authorized_ogrn.value.trim(), members }) });
      await load(); fill(); if (onSaved) await onSaved(); showToast("Группа сохранена", "success");
    } catch (err) { error.textContent = err.message; }
    finally { button.disabled = false; }
  };
  document.body.appendChild(modal);
  try { await load(); } catch (error) { modal.querySelector("#group-list").textContent = error.message; }
}

const MINISTRY_EDUCATION_DIRECTORY_URL = "https://adm.digital.gov.ru/app/uploads/2026/05/6027f0_perechen-oo-vo-realizuyushhih-it-speczialnosti.pdf";

async function openDirectoryCreate(onSaved = async () => {}) {
  if (!canProposeEducationDirectory() && !canApproveEducationDirectory()) return;
  const admin = canApproveEducationDirectory();
  const today = new Date().toISOString().slice(0, 10);
  const modal = el(`<div class="modal-backdrop"><form class="modal modal-wide" role="dialog" aria-modal="true">
    <div class="flex between"><div><span class="eyebrow">${admin ? "Новая запись" : "Предложение модератора"}</span><h2>Добавить учебное заведение</h2></div><button class="btn secondary" type="button" data-close>Закрыть</button></div>
    <p class="notice">Основание — официальный перечень образовательных организаций Минцифры. Для подтверждения администратором дополнительно нужны актуальные реквизиты и действующая лицензия.</p>
    <div class="grid cols-2">
      <div class="field"><label>Полное наименование *</label><input name="name" required minlength="2" maxlength="1000"></div>
      <div class="field"><label>Тип организации *</label><select name="partner_kind"><option value="vuz">Вуз</option><option value="kolledj">Колледж / СПО</option><option value="school">Школа</option></select></div>
      <div class="field"><label>Регион *</label><input name="region" required minlength="2" maxlength="200"></div>
      <div class="field"><label>Дата проверки источника *</label><input name="registry_updated_at" type="date" max="${today}" value="${today}" required></div>
      <div class="field"><label>ИНН</label><input name="inn" inputmode="numeric" pattern="[0-9]{10}|[0-9]{12}"></div>
      <div class="field"><label>ОГРН / ОГРНИП</label><input name="ogrn" inputmode="numeric" pattern="[0-9]{13}|[0-9]{15}"></div>
      <div class="field"><label>Номер лицензии</label><input name="license_number" maxlength="100"></div>
      <div class="field"><label>Статус лицензии</label><select name="license_status"><option value="unknown">Требует проверки</option><option value="active">Действует</option><option value="suspended">Приостановлена</option><option value="expired">Истекла</option><option value="revoked">Аннулирована</option></select></div>
      <div class="field"><label>Статус организации</label><select name="institution_status"><option value="unknown">Требует проверки</option><option value="active">Действует</option><option value="inactive">Не действует</option><option value="reorganized">Реорганизована</option><option value="liquidated">Ликвидирована</option></select></div>
      <div class="field"><label>Коды ИТ-направлений *</label><input name="program_codes" required placeholder="09.03.01, 10.05.01"></div>
      <div class="field"><label>Официальный перечень *</label><input name="source_url" type="url" required value="${MINISTRY_EDUCATION_DIRECTORY_URL}"></div>
      <div class="field"><label>Источник направлений *</label><input name="programs_source_url" type="url" required value="${MINISTRY_EDUCATION_DIRECTORY_URL}"></div>
    </div>
    <div class="field"><label>${admin ? "Комментарий" : "Почему организацию нужно добавить *"}</label><textarea name="proposal_comment" rows="2" maxlength="1000" ${admin ? "" : "required minlength=5"} placeholder="Укажите строку или сведения из официального перечня"></textarea></div>
    <p class="error" role="alert"></p>
    <div class="flex">${admin ? '<button class="btn secondary" type="submit" data-mode="save">Создать для проверки</button><button class="btn" type="submit" data-mode="confirm">Создать и подтвердить</button>' : '<button class="btn" type="submit" data-mode="propose">Отправить администратору</button>'}</div>
  </form></div>`);
  document.body.appendChild(modal);
  const form = modal.querySelector("form");
  modal.querySelector("[data-close]").onclick = () => modal.remove();
  form.onsubmit = async (event) => {
    event.preventDefault();
    if (!form.reportValidity()) return;
    const buttons = form.querySelectorAll("button");
    buttons.forEach((button) => (button.disabled = true));
    form.querySelector(".error").textContent = "";
    try {
      const payload = Object.fromEntries(new FormData(form));
      payload.confirm = event.submitter?.dataset.mode === "confirm";
      payload.program_codes = payload.program_codes.split(/[,;\s]+/).filter(Boolean);
      await api("/directory", { method: "POST", body: JSON.stringify(payload) });
      modal.remove();
      await onSaved();
      showToast(admin ? (payload.confirm ? "Учебное заведение добавлено и подтверждено" : "Учебное заведение добавлено для проверки") : "Предложение отправлено администратору", "success");
    } catch (error) {
      form.querySelector(".error").textContent = error.message;
      buttons.forEach((button) => (button.disabled = false));
    }
  };
}

function openDirectoryProposalRejection(item, onSaved) {
  const modal = el(`<div class="modal-backdrop"><form class="modal" role="dialog" aria-modal="true"><span class="eyebrow">Решение администратора</span><h2>Отклонить предложение</h2><p><b>${escapeHTML(item.name)}</b></p><div class="field"><label>Причина отклонения *</label><textarea name="comment" required minlength="5" maxlength="1000" rows="3"></textarea></div><p class="error" role="alert"></p><div class="flex"><button class="btn danger" type="submit">Отклонить</button><button class="btn secondary" type="button">Отмена</button></div></form></div>`);
  document.body.appendChild(modal);
  const form = modal.querySelector("form");
  form.querySelector('[type="button"]').onclick = () => modal.remove();
  form.onsubmit = async (event) => {
    event.preventDefault();
    if (!form.reportValidity()) return;
    const button = form.querySelector('[type="submit"]');
    button.disabled = true;
    try {
      await api(`/directory/${encodeURIComponent(item.id)}/decision`, { method: "POST", body: JSON.stringify({ decision: "reject", comment: form.elements.comment.value.trim() }) });
      modal.remove();
      await onSaved();
      showToast("Предложение отклонено", "success");
    } catch (error) {
      form.querySelector(".error").textContent = error.message;
      button.disabled = false;
    }
  };
}

async function loadDirectoryProposals(root, onSaved) {
  const box = root.querySelector("#directory-proposals");
  if (!box) return;
  const proposals = await api("/directory/proposals");
  const pending = proposals.filter((item) => item.verification_status === "pending");
  const visible = canApproveEducationDirectory() ? pending : proposals.slice(0, 20);
  box.className = `card proposal-center${pending.length ? " has-pending" : ""}`;
  box.innerHTML = `<div class="flex between"><div><span class="eyebrow">${canApproveEducationDirectory() ? "Требуют решения" : "Мои предложения"}</span><h2>${pending.length ? `${pending.length} ${canApproveEducationDirectory() ? "новых предложений" : "ожидают проверки"}` : "Новых предложений нет"}</h2></div><span class="proposal-count">${pending.length}</span></div>${visible.length ? `<div class="proposal-list">${visible.map((item) => `<article class="proposal-item"><div><b>${escapeHTML(item.name)}</b><span>${escapeHTML(AUDIENCE_LABELS[item.partner_kind] || item.partner_kind)} · ${escapeHTML(item.region)}</span><small>${escapeHTML(item.submitter_name)} · ${new Date(item.proposed_at).toLocaleString("ru-RU")}</small><p>${escapeHTML(item.proposal_comment || "Без комментария")}</p></div><div><span class="status-badge ${item.verification_status === "verified" ? "active" : item.verification_status === "rejected" ? "inactive" : "pending"}">${item.verification_status === "verified" ? "Принято" : item.verification_status === "rejected" ? "Отклонено" : "На рассмотрении"}</span>${canApproveEducationDirectory() && item.verification_status === "pending" ? `<button class="btn" data-proposal-review="${item.id}">Проверить и принять</button><button class="btn secondary" data-proposal-reject="${item.id}">Отклонить</button>` : ""}${item.review_comment ? `<small>${escapeHTML(item.review_comment)}</small>` : ""}</div></article>`).join("")}</div>` : '<p class="muted">Здесь появятся организации, которые модераторы отправят на подтверждение.</p>'}`;
  box.querySelectorAll("[data-proposal-review]").forEach((button) => {
    button.onclick = () => openDirectoryReview(proposals.find((item) => item.id === button.dataset.proposalReview), onSaved);
  });
  box.querySelectorAll("[data-proposal-reject]").forEach((button) => {
    button.onclick = () => openDirectoryProposalRejection(proposals.find((item) => item.id === button.dataset.proposalReject), onSaved);
  });
}

async function refreshDirectoryProposalBadge(root = document) {
  const badges = [...root.querySelectorAll("[data-directory-proposal-count]")];
  if (!badges.length || (!canProposeEducationDirectory() && !canApproveEducationDirectory())) return;
  try {
    const stats = await api("/directory/stats");
    const count = canApproveEducationDirectory()
      ? Number(stats.pending_proposals || 0)
      : Number(stats.own_pending_proposals || 0);
    badges.forEach((badge) => {
      badge.textContent = count;
      badge.hidden = count === 0;
      badge.setAttribute("aria-label", `${count} предложений ожидают решения`);
      badge.title = `${count} предложений ожидают решения`;
    });
  } catch (_) {
    badges.forEach((badge) => { badge.hidden = true; });
  }
}

async function renderPartnerDirectory(root, embedded = false) {
  if (!canReviewEducationDirectory()) {
    root.innerHTML = '<div class="card error-state">Справочник учебных заведений недоступен для этого профиля</div>';
    return;
  }
  const canSuggest = canProposeEducationDirectory();
  const canApprove = canApproveEducationDirectory();
  state.regionalAuthorities = await api("/regional-authorities");
  root.innerHTML = `${embedded ? '<div class="section-intro"><h2>Учебные заведения и соглашения</h2></div>' : '<section class="page-heading"><div><h1>Учебные заведения и соглашения</h1></div></section>'}
  ${isStaffUser() && state.me?.it_company_id ? '<div class="card"><div class="flex between"><div><h2>Группа юридических лиц</h2><p class="muted">Договор взаимодействия, уполномоченное лицо и участники для консолидированного плана и отчёта.</p></div><button class="btn secondary" id="legal-groups">Управлять группами</button></div></div>' : ""}
  ${canSuggest || canApprove ? '<div id="directory-proposals" class="card proposal-center"><div class="loading-state"><span class="spinner"></span>Загрузка предложений…</div></div>' : ""}
  <div class="card"><h2>Состояние справочника</h2><div id="directory-stats">Загрузка…</div><details class="rules-note"><summary>Что требует проверки</summary><p>Для записей с жёлтым статусом сверьте ИНН и ОГРН, затем подтвердите реквизиты. Партнёром может стать организация с действующей лицензией.</p></details></div>
  <div class="card"><div class="flex between directory-heading"><div><h2>Поиск в официальном справочнике</h2><p class="muted">Официальный источник для проверки — опубликованный Минцифры перечень образовательных организаций.</p></div>${canSuggest || canApprove ? `<button class="btn" id="directory-add">+ ${canApprove ? "Добавить ОО" : "Предложить ОО"}</button>` : ""}</div><div class="grid cols-3"><div class="field"><label>Тип ОО</label><select id="d-kind">${Object.entries(
    AUDIENCE_LABELS,
  )
    .map(
      ([k, v]) =>
        `<option value="${k}" ${k === state.partnerKind ? "selected" : ""}>${v}</option>`,
    )
    .join(
      "",
    )}</select></div><div class="field"><label>Название, регион, ИНН, ОГРН или лицензия</label><input id="d-search" placeholder="Поиск"></div><button class="btn secondary" id="d-find">Найти</button></div><div id="d-results"></div>${canApprove ? '<button class="btn secondary" id="d-import">Редактировать и подтверждать через Excel</button>' : ""}</div>
  <div class="card"><div class="flex between"><h2>Региональные органы управления образованием</h2>${isStaffUser() ? '<button class="btn secondary" id="roiv-add">+ Добавить РОИВ</button>' : ""}</div><div id="roiv-list">${state.regionalAuthorities.length ? `<div class="table-wrap"><table><thead><tr><th>Регион и РОИВ</th><th>Реквизиты</th><th>Связи</th><th></th></tr></thead><tbody>${state.regionalAuthorities.map((authority) => `<tr><td>${escapeHTML(authority.region)}<br><b>${escapeHTML(authority.name)}</b></td><td>ИНН ${escapeHTML(authority.inn)}<br>ОГРН ${escapeHTML(authority.ogrn)}<br><a href="${escapeHTML(authority.source_url)}" target="_blank" rel="noopener noreferrer">Официальный источник</a></td><td><span class="status-badge ${authority.status === "active" ? "active" : "inactive"}">${authority.status === "active" ? "Действует" : "Не действует"}</span><br>Школ: ${authority.schools_count}<br>Мероприятий: ${authority.activities_count}</td><td>${isStaffUser() ? `<button class="btn secondary" data-edit-roiv="${authority.id}">Изменить</button>` : ""}</td></tr>`).join("")}</tbody></table></div>` : "<p>РОИВ ещё не добавлены. Сначала добавьте РОИВ, затем школьного партнёра.</p>"}</div></div>
  <div class="card"><div class="flex between"><h2>Наши партнёры</h2><span class="count-badge" id="partner-count"></span></div><div class="grid cols-3"><div class="field"><label>Вид учебного заведения</label><select id="partner-kind-filter"><option value="">Все виды</option>${Object.entries(AUDIENCE_LABELS).map(([code, label]) => `<option value="${code}">${escapeHTML(label)}</option>`).join("")}</select></div><div class="field"><label>Название или ИНН</label><input id="partner-name-filter" placeholder="Начните вводить название или ИНН"></div><button class="btn secondary" id="partner-filter-reset">Сбросить</button></div><div id="partner-list"></div></div>${isStaffUser() ? '<div id="partner-create"></div>' : ""}`;
  let generation = 0;
  const reviewFilter = el('<label class="muted"><input type="checkbox" id="d-review-all"> Показать также вузы без подтверждённого направления — для проверки</label>');
  root.querySelector("#d-results").before(reviewFilter);
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
            review_all: root.querySelector("#d-review-all").checked ? "1" : "0",
          }),
      );
      if (version !== generation) return;
      box.innerHTML = items.length
        ? `<p class="muted">Найдено записей: ${items.length}.</p><div class="table-wrap"><table><thead><tr><th>Название</th><th>Реквизиты</th><th>Проверка и лицензия</th><th>Действия</th></tr></thead><tbody>${items.map((p) => {
          const pending = p.verification_status !== "verified";
          const verificationLabel = pending ? "Требует проверки" : "Подтверждено";
          const verifier = !pending && (p.verifier_name || p.verifier_email)
            ? `<br><small>${escapeHTML(p.verifier_name || p.verifier_email)}${p.verified_at ? ` · ${new Date(p.verified_at).toLocaleString("ru-RU")}` : ""}</small>`
            : "";
          return `<tr><td>${escapeHTML(p.name)}${p.listed_in_mincifry_order_27 ? ' <span class="status-badge active">Перечень № 27</span>' : ""}<br><small>${escapeHTML(p.region)}</small>${p.partner_kind === "vuz" ? `<br><small>${p.matches_order ? `Подходящие направления: ${escapeHTML((p.matching_program_codes || []).join(", "))}` : (p.program_codes || []).length ? "Подходящих направлений по приказу нет" : "Направления ещё не получены"}</small>${p.programs_source_url ? `<br><a href="${escapeHTML(p.programs_source_url)}" target="_blank" rel="noopener noreferrer">Образовательные программы</a>` : ""}` : ""}</td><td>ИНН ${escapeHTML(p.inn || "—")}<br>ОГРН ${escapeHTML(p.ogrn || "—")}</td><td><span class="status-badge ${pending ? "pending" : "active"}">${verificationLabel}</span>${verifier}<br>${escapeHTML(p.license_number || "Лицензия не указана")} · ${escapeHTML(directoryStatusLabel(p.license_status))}<br><small>${escapeHTML(p.registry_updated_at || p.source)}</small></td><td>${canApprove ? `<button class="btn secondary" data-review-directory="${p.id}">Редактировать / подтвердить</button>` : ""}${isStaffUser() ? `<button class="btn secondary" data-directory="${p.id}" ${p.selectable ? "" : "disabled"}>${p.selectable ? "Выбрать" : "Недоступно для соглашения"}</button>` : ""}${p.source_url ? `<br><a href="${escapeHTML(p.source_url)}" target="_blank" rel="noopener noreferrer">Источник</a>` : ""}</td></tr>`;
        }).join("")}</tbody></table></div>`
        : `<p>${root.querySelector("#d-kind").value === "vuz" ? "Вузы с найденными подходящими направлениями не найдены. Для проверки записи включите «Показать также вузы без подтверждённого направления» или измените запрос." : "Совпадений нет. Измените запрос или загрузите сведения в справочник."}</p>`;
	  box.querySelectorAll("[data-review-directory]").forEach((button) => {
	    button.onclick = () => openDirectoryReview(
	      items.find((item) => item.id === button.dataset.reviewDirectory),
	      search,
	    );
	  });
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
  root.querySelector("#directory-add")?.addEventListener("click", () =>
    openDirectoryCreate(async () => {
      await Promise.all([search(), loadDirectoryProposals(root, () => renderPartnerDirectory(root, embedded))]);
      await refreshDirectoryProposalBadge(document);
    }),
  );
  root.querySelector("#legal-groups")?.addEventListener("click", () => openLegalEntityGroups(async () => {
    state.legalEntityGroups = await api("/legal-entity-groups");
  }));
  root.querySelector("#d-review-all").onchange = search;
  root.querySelector("#d-kind").onchange = () => {
    state.partnerKind = root.querySelector("#d-kind").value;
    search();
  };
  root.querySelector("#d-search").onkeydown = (e) => {
    if (e.key === "Enter") search();
  };
  root
    .querySelector("#d-import")
    ?.addEventListener("click", () => openImportDialog(true, () => renderPartnerDirectory(root, embedded)));
  root
    .querySelector("#roiv-add")
    ?.addEventListener("click", () =>
      openRegionalAuthority(null, () => renderPartnerDirectory(root, embedded)),
    );
  root.querySelectorAll("[data-edit-roiv]").forEach((button) => {
    button.onclick = () =>
      openRegionalAuthority(
        state.regionalAuthorities.find(
          (item) => item.id === button.dataset.editRoiv,
        ),
        () => renderPartnerDirectory(root, embedded),
      );
  });
  if (canSuggest || canApprove) {
    loadDirectoryProposals(root, async () => {
      await renderPartnerDirectory(root, embedded);
      await refreshDirectoryProposalBadge(document);
    }).catch((error) => {
      const box = root.querySelector("#directory-proposals");
      if (box) box.innerHTML = `<p class="error">${escapeHTML(error.message)}</p>`;
    });
  }
  api("/directory/stats")
    .then((stats) => {
      const box = root.querySelector("#directory-stats");
      if (!box) return;
      box.innerHTML = `<div class="grid cols-2"><div class="stat"><div class="label">Организации из перечня</div><div class="value">${stats.all_total}</div></div><div class="stat warning-stat"><div class="label">Требуют проверки реквизитов</div><div class="value">${stats.pending || 0}</div></div><div class="stat"><div class="label">Подтверждены и действуют</div><div class="value">${stats.verified_active}</div></div><div class="stat"><div class="label">С подходящими направлениями</div><div class="value">${stats.total}</div></div></div><p class="muted">Вузов из перечня Минцифры № 27: ${stats.all_universities}. Направления ещё не получены: ${stats.programs_unknown}. Последнее подтверждение: ${stats.last_verified_at ? new Date(stats.last_verified_at).toLocaleString("ru-RU") : "записей пока нет"}. ${stats.sync_status ? `Обогащение/обновление: ${escapeHTML(stats.sync_status)}${stats.sync_finished_at ? `, ${new Date(stats.sync_finished_at).toLocaleString("ru-RU")}` : ""}${stats.sync_error ? ` — ${escapeHTML(stats.sync_error)}` : ""}.` : "Автоматическое обогащение ещё не запускалось."}</p>`;
    })
    .catch((error) => {
      const box = root.querySelector("#directory-stats");
      if (box) box.textContent = error.message;
    });
  const refreshPartners = async () => {
    state.partners = await api("/partners");
    paintPartners();
  };
  const paintPartners = () => {
    const kind = root.querySelector("#partner-kind-filter").value;
    const term = root.querySelector("#partner-name-filter").value.trim().toLocaleLowerCase("ru");
    const partners = state.partners.filter((partner) =>
      (!kind || partner.partner_kind === kind) &&
      (!term || partner.name.toLocaleLowerCase("ru").includes(term) || String(partner.inn || "").includes(term)),
    );
    root.querySelector("#partner-count").textContent = `Показано: ${partners.length} из ${state.partners.length}`;
    root.querySelector("#partner-list").innerHTML = partners.length
      ? `<div class="table-wrap"><table><thead><tr><th>Учебное заведение</th><th>Проверка</th><th>Соглашения</th><th>Учебная структура</th><th></th></tr></thead><tbody>${partners.map((p) => `<tr><td>${escapeHTML(p.name)}<br><small>${escapeHTML(AUDIENCE_LABELS[p.partner_kind])}</small></td><td><span class="status-badge ${p.verification_status === "verified" ? "active" : "inactive"}">${p.verification_status === "verified" ? "Реестр подтверждён" : "Историческая запись"}</span><br><small>${escapeHTML(p.inn || "ИНН не указан")}</small></td><td>Всего: ${p.agreements_count}<br>Действующих сейчас: ${p.active_agreements_count}</td><td>Подразделений: ${Number(p.org_units_count || 0)}<br>Групп: ${Number(p.academic_groups_count || 0)}</td><td><button class="btn secondary" data-partner="${p.id}">План / факт</button><button class="btn secondary" data-agreements="${p.id}">Соглашения</button><button class="btn secondary" data-structure="${p.id}" ${p.partner_kind === "school" ? "disabled" : ""}>Структура и группы</button><button class="btn secondary" data-mentors="${p.id}">Наставники</button></td></tr>`).join("")}</tbody></table></div>`
      : '<p class="muted">Партнёры по выбранным условиям не найдены.</p>';
    root.querySelectorAll("[data-partner]").forEach((button) => {
      button.onclick = () => {
        state.partnerID = button.dataset.partner;
        CyberCalcRouter.activate("teachers");
      };
    });
    root.querySelectorAll("[data-mentors]").forEach((button) => {
      button.onclick = () => openMentors(button.dataset.mentors);
    });
    root.querySelectorAll("[data-structure]").forEach((button) => {
      if (!button.disabled) button.onclick = () => openPartnerStructure(button.dataset.structure, refreshPartners);
    });
    root.querySelectorAll("[data-agreements]").forEach((button) => {
      button.onclick = () => openAgreements(button.dataset.agreements, refreshPartners);
    });
  };
  root.querySelector("#partner-kind-filter").onchange = paintPartners;
  root.querySelector("#partner-name-filter").oninput = paintPartners;
  root.querySelector("#partner-filter-reset").onclick = () => {
    root.querySelector("#partner-kind-filter").value = "";
    root.querySelector("#partner-name-filter").value = "";
    paintPartners();
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
        await renderPartnerDirectory(root, embedded);
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

const ORG_UNIT_LABELS = {
  institute: "Институт",
  faculty: "Факультет",
  division: "Отделение",
  department: "Кафедра",
  laboratory: "Лаборатория",
};

const EDUCATION_LEVEL_LABELS = {
  vo_bachelor: "ВО — бакалавриат",
  vo_master: "ВО — магистратура",
  vo_specialist: "ВО — специалитет",
  spo: "СПО",
};

async function openPartnerStructure(partnerID, onSaved = async () => {}) {
  const partner = state.partners.find((item) => item.id === partnerID);
  if (!partner) return;
  const editable = canManageAcademicStructure();
  const modal = el(`<div class="modal-backdrop"><div class="modal modal-wide" role="dialog" aria-modal="true" aria-label="Структура учебного заведения"><div class="flex between"><div><span class="eyebrow">Карточка партнёра</span><h2>Структура и группы: ${escapeHTML(partner.name)}</h2></div><button class="btn secondary" id="structure-close">Закрыть</button></div><div class="tabs"><button class="active" data-structure-tab="units">Подразделения</button><button data-structure-tab="groups">Академические группы</button></div><section id="structure-units"></section><section id="structure-groups" hidden></section></div></div>`);
  document.body.appendChild(modal);
  modal.querySelector("#structure-close").onclick = () => modal.remove();
  modal.querySelectorAll("[data-structure-tab]").forEach((button) => {
    button.onclick = () => {
      modal.querySelectorAll("[data-structure-tab]").forEach((item) => item.classList.toggle("active", item === button));
      modal.querySelector("#structure-units").hidden = button.dataset.structureTab !== "units";
      modal.querySelector("#structure-groups").hidden = button.dataset.structureTab !== "groups";
    };
  });

  let units = [];
  let groups = [];
  let specialtyCodes = [];
  let editingUnitID = "";
  let editingGroupID = "";

  const unitFields = (item = {}) => `<div class="grid cols-3"><div class="field"><label>Родительское подразделение</label><select name="parent_unit_id"><option value="">— Корневой уровень —</option>${units.map((unit) => `<option value="${unit.id}" ${unit.id === item.parent_unit_id ? "selected" : ""} ${unit.id === item.id ? "disabled" : ""}>${"— ".repeat(Math.max(0, unit.depth - 1))}${escapeHTML(unit.unit_name)}</option>`).join("")}</select></div><div class="field"><label>Тип *</label><select name="unit_level_type" required>${Object.entries(ORG_UNIT_LABELS).map(([code, label]) => `<option value="${code}" ${code === item.unit_level_type ? "selected" : ""}>${label}</option>`).join("")}</select></div><div class="field"><label>Наименование *</label><input name="unit_name" required minlength="2" maxlength="300" value="${escapeHTML(item.unit_name || "")}"></div><div class="field"><label>Руководитель</label><input name="head_fio" maxlength="200" value="${escapeHTML(item.head_fio || "")}"></div><div class="field"><label>Должность руководителя</label><input name="head_position" maxlength="200" value="${escapeHTML(item.head_position || "")}"></div><div class="field"><label>Контакты руководителя</label><input name="head_contacts" maxlength="500" value="${escapeHTML(item.head_contacts || "")}"></div><div class="field"><label>Заведующий кафедрой</label><input name="chair_fio" maxlength="200" value="${escapeHTML(item.chair_fio || "")}"></div><div class="field"><label>Контакты заведующего</label><input name="chair_contacts" maxlength="500" value="${escapeHTML(item.chair_contacts || "")}"></div><div class="field"><label>Куратор от ОО</label><input name="curator_fio" maxlength="200" value="${escapeHTML(item.curator_fio || "")}"></div><div class="field"><label>Контакты куратора</label><input name="curator_contacts" maxlength="500" value="${escapeHTML(item.curator_contacts || "")}"></div></div>`;

  const groupFields = (item = {}) => {
    const levels = partner.partner_kind === "kolledj" ? [["spo", "СПО"]] : Object.entries(EDUCATION_LEVEL_LABELS).filter(([code]) => code !== "spo");
    return `<div class="grid cols-3"><div class="field"><label>Подразделение *</label><select name="unit_id" required><option value="">— Выберите —</option>${units.map((unit) => `<option value="${unit.id}" ${unit.id === item.unit_id ? "selected" : ""}>${"— ".repeat(Math.max(0, unit.depth - 1))}${escapeHTML(unit.unit_name)}</option>`).join("")}</select></div><div class="field"><label>Шифр группы *</label><input name="group_name" required maxlength="100" value="${escapeHTML(item.group_name || "")}" placeholder="БПИ-231"></div><div class="field"><label>Уровень образования *</label><select name="education_level" required>${levels.map(([code, label]) => `<option value="${code}" ${code === item.education_level ? "selected" : ""}>${label}</option>`).join("")}</select></div><div class="field"><label>Курс *</label><input name="course_num" type="number" min="1" max="7" required value="${Number(item.course_num || 1)}"></div><div class="field"><label>Сквозной семестр *</label><input name="current_semester" type="number" min="1" max="13" required value="${Number(item.current_semester || 1)}"></div><div class="field"><label>Период *</label><select name="semester_period"><option value="autumn" ${item.semester_period !== "spring" ? "selected" : ""}>Осень</option><option value="spring" ${item.semester_period === "spring" ? "selected" : ""}>Весна</option></select></div><div class="field"><label>Код специальности по Приказу № 27 *</label><input name="specialty_code" required pattern="[0-9]{2}\\.[0-9]{2}\\.[0-9]{2}" maxlength="8" value="${escapeHTML(item.specialty_code || "")}" list="partner-specialty-codes" placeholder="09.03.01"><datalist id="partner-specialty-codes">${specialtyCodes.map((code) => `<option value="${escapeHTML(code)}"></option>`).join("")}</datalist></div><div class="field"><label>Численность студентов *</label><input name="students_count" type="number" min="0" max="10000" required value="${Number(item.students_count || 0)}"></div></div>`;
  };

  const collectUnit = (form) => Object.fromEntries(["parent_unit_id", "unit_level_type", "unit_name", "head_fio", "head_position", "head_contacts", "chair_fio", "chair_contacts", "curator_fio", "curator_contacts"].map((name) => [name, form.elements[name].value.trim()]));
  const collectGroup = (form) => ({
    unit_id: form.elements.unit_id.value,
    group_name: form.elements.group_name.value.trim(),
    education_level: form.elements.education_level.value,
    course_num: Number(form.elements.course_num.value),
    current_semester: Number(form.elements.current_semester.value),
    semester_period: form.elements.semester_period.value,
    specialty_code: form.elements.specialty_code.value.trim(),
    students_count: Number(form.elements.students_count.value),
  });

  const renderUnits = () => {
    const section = modal.querySelector("#structure-units");
    section.innerHTML = `<div class="card"><div class="flex between"><div><h2>Иерархия подразделений</h2><p class="muted">До 10 уровней; циклические и межорганизационные связи блокируются сервером.</p></div><span class="count-badge">${units.length}</span></div>${units.length ? `<div class="table-wrap"><table><thead><tr><th>Подразделение</th><th>Тип</th><th>Руководитель</th><th>Куратор ОО</th>${editable ? "<th></th>" : ""}</tr></thead><tbody>${units.map((unit) => `<tr><td style="padding-left:${10 + Math.max(0, unit.depth - 1) * 18}px"><b>${escapeHTML(unit.unit_name)}</b>${unit.parent_unit_name ? `<br><small>В составе: ${escapeHTML(unit.parent_unit_name)}</small>` : ""}</td><td>${escapeHTML(ORG_UNIT_LABELS[unit.unit_level_type] || unit.unit_level_type)}</td><td>${escapeHTML(unit.head_fio || unit.chair_fio || "—")}<br><small>${escapeHTML(unit.head_contacts || unit.chair_contacts || "")}</small></td><td>${escapeHTML(unit.curator_fio || "—")}<br><small>${escapeHTML(unit.curator_contacts || "")}</small></td>${editable ? `<td><button class="btn secondary" data-edit-unit="${unit.id}">Изменить</button><button class="btn secondary" data-delete-unit="${unit.id}">Удалить</button></td>` : ""}</tr>`).join("")}</tbody></table></div>` : '<p class="muted">Подразделения ещё не внесены.</p>'}</div>${editable ? `<form class="card" id="unit-form"><h2>${editingUnitID ? "Изменить подразделение" : "Добавить подразделение"}</h2>${unitFields(units.find((unit) => unit.id === editingUnitID) || {})}<p class="error" role="alert"></p><div class="flex"><button class="btn" type="submit">Сохранить</button>${editingUnitID ? '<button class="btn secondary" type="button" data-unit-reset>Отмена</button>' : ""}</div></form>` : ""}`;
    section.querySelectorAll("[data-edit-unit]").forEach((button) => { button.onclick = () => { editingUnitID = button.dataset.editUnit; renderUnits(); }; });
    section.querySelectorAll("[data-delete-unit]").forEach((button) => { button.onclick = async () => {
      if (!confirm("Удалить подразделение? Подразделение с дочерними элементами или группами удалить нельзя.")) return;
      try { await api(`/org-units/${encodeURIComponent(button.dataset.deleteUnit)}`, {method: "DELETE"}); await load(); showToast("Подразделение удалено", "success"); } catch (error) { showToast(error.message, "error"); }
    }; });
    section.querySelector("[data-unit-reset]")?.addEventListener("click", () => { editingUnitID = ""; renderUnits(); });
    const form = section.querySelector("#unit-form");
    if (form) form.onsubmit = async (event) => {
      event.preventDefault();
      if (!form.reportValidity()) return;
      const button = form.querySelector('[type="submit"]'); button.disabled = true;
      try {
        await api(editingUnitID ? `/org-units/${encodeURIComponent(editingUnitID)}` : `/partners/${encodeURIComponent(partnerID)}/org-units`, {method: editingUnitID ? "PUT" : "POST", body: JSON.stringify(collectUnit(form))});
        editingUnitID = ""; await load(); showToast("Структура сохранена", "success");
      } catch (error) { form.querySelector(".error").textContent = error.message; button.disabled = false; }
    };
  };

  const renderGroups = () => {
    const section = modal.querySelector("#structure-groups");
    section.innerHTML = `<div class="card"><div class="flex between"><div><h2>Академические группы</h2><p class="muted">Группа связана с подразделением, уровнем образования и специальностью партнёра.</p></div><span class="count-badge">${groups.length} групп · ${groups.reduce((sum, group) => sum + Number(group.students_count || 0), 0)} студентов</span></div>${groups.length ? `<div class="table-wrap"><table><thead><tr><th>Группа</th><th>Подразделение</th><th>Уровень</th><th>Курс / семестр</th><th>Специальность</th><th>Студентов</th>${editable ? "<th></th>" : ""}</tr></thead><tbody>${groups.map((group) => `<tr><td><b>${escapeHTML(group.group_name)}</b></td><td>${escapeHTML(group.unit_name)}</td><td>${escapeHTML(EDUCATION_LEVEL_LABELS[group.education_level] || group.education_level)}</td><td>${group.course_num} курс · ${group.current_semester} семестр<br><small>${group.semester_period === "spring" ? "Весна" : "Осень"}</small></td><td>${escapeHTML(group.specialty_code)}</td><td>${group.students_count}</td>${editable ? `<td><button class="btn secondary" data-edit-group="${group.id}">Изменить</button><button class="btn secondary" data-delete-group="${group.id}">Удалить</button></td>` : ""}</tr>`).join("")}</tbody></table></div>` : '<p class="muted">Академические группы ещё не внесены.</p>'}</div>${editable ? `<form class="card" id="group-form"><h2>${editingGroupID ? "Изменить группу" : "Добавить группу"}</h2>${units.length ? groupFields(groups.find((group) => group.id === editingGroupID) || {}) : '<p class="notice">Сначала добавьте хотя бы одно структурное подразделение.</p>'}<p class="error" role="alert"></p>${units.length ? `<div class="flex"><button class="btn" type="submit">Сохранить</button>${editingGroupID ? '<button class="btn secondary" type="button" data-group-reset>Отмена</button>' : ""}</div>` : ""}</form>` : ""}`;
    section.querySelectorAll("[data-edit-group]").forEach((button) => { button.onclick = () => { editingGroupID = button.dataset.editGroup; renderGroups(); }; });
    section.querySelectorAll("[data-delete-group]").forEach((button) => { button.onclick = async () => {
      if (!confirm("Удалить академическую группу?")) return;
      try { await api(`/academic-groups/${encodeURIComponent(button.dataset.deleteGroup)}`, {method: "DELETE"}); await load(); showToast("Группа удалена", "success"); } catch (error) { showToast(error.message, "error"); }
    }; });
    section.querySelector("[data-group-reset]")?.addEventListener("click", () => { editingGroupID = ""; renderGroups(); });
    const form = section.querySelector("#group-form");
    if (form && units.length) form.onsubmit = async (event) => {
      event.preventDefault();
      if (!form.reportValidity()) return;
      const button = form.querySelector('[type="submit"]'); button.disabled = true;
      try {
        await api(editingGroupID ? `/academic-groups/${encodeURIComponent(editingGroupID)}` : `/partners/${encodeURIComponent(partnerID)}/academic-groups`, {method: editingGroupID ? "PUT" : "POST", body: JSON.stringify(collectGroup(form))});
        editingGroupID = ""; await load(); showToast("Академическая группа сохранена", "success");
      } catch (error) { form.querySelector(".error").textContent = error.message; button.disabled = false; }
    };
  };

  const load = async () => {
    [units, groups, specialtyCodes] = await Promise.all([
      api(`/partners/${encodeURIComponent(partnerID)}/org-units`),
      api(`/partners/${encodeURIComponent(partnerID)}/academic-groups`),
      api(`/partners/${encodeURIComponent(partnerID)}/specialty-codes`),
    ]);
    renderUnits(); renderGroups(); await onSaved();
  };
  try { await load(); } catch (error) { modal.querySelector("#structure-units").innerHTML = `<div class="card error">${escapeHTML(error.message)}</div>`; }
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
      ? agreements.map((agreement) => `<div class="agreement-card"><div><b>${escapeHTML(agreementLabel(agreement))}</b><br>${escapeHTML(AGREEMENT_KIND_LABELS[agreement.agreement_kind])}${agreement.roiv_name ? `: ${escapeHTML(agreement.roiv_name)}` : ""}<br><small>Подписание: ${escapeHTML(agreement.signature_method)}${agreement.signed_by ? ` · ${escapeHTML(agreement.signed_by)}` : ""}. Организаций: ${agreement.partner_ids.length}. Ответственных: ${agreement.responsible_people.length}. Виды мероприятий: ${(agreement.activity_codes || []).length}.</small></div>${isStaffUser() ? `<button class="btn secondary" data-edit-agreement="${agreement.id}">Изменить</button>` : ""}</div>`).join("")
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
