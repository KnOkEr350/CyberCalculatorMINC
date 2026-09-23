(function installCyberCalcUI(global) {
  "use strict";

  function escapeHTML(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#039;");
  }

  function attributes(values) {
    return Object.entries(values)
      .filter(([, value]) => value !== undefined && value !== null && value !== false)
      .map(([name, value]) => value === true ? ` ${name}` : ` ${name}="${escapeHTML(value)}"`)
      .join("");
  }

  function formatMoney(value) {
    const amount = Number(value || 0);
    const safe = Number.isFinite(amount) ? amount : 0;
    return `${safe.toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽`;
  }

  function moneyInput(options = {}) {
    const id = options.id || options.name || "money";
    return `<div class="field ui-money-field"><label for="${escapeHTML(id)}">${escapeHTML(options.label || "Сумма, ₽")}${options.required ? " *" : ""}</label><input${attributes({
      id, name: options.name || id, type: "text", inputmode: "decimal", value: options.value ?? "",
      placeholder: options.placeholder || "0,00", required: options.required, disabled: options.disabled,
      "aria-describedby": options.hint ? `${id}-hint` : undefined,
    })} pattern="^(0|[1-9][0-9]*)([.,][0-9]{1,2})?$">${options.hint ? `<div class="field-hint" id="${escapeHTML(id)}-hint">${escapeHTML(options.hint)}</div>` : ""}</div>`;
  }

  function parseMoney(value) {
    const normalized = String(value ?? "").trim().replace(/\s/g, "").replace(",", ".");
    if (!/^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$/.test(normalized)) return null;
    const amount = Number(normalized);
    return Number.isSafeInteger(Math.round(amount * 100)) ? Math.round(amount * 100) : null;
  }

  function dateInput(options = {}) {
    const id = options.id || options.name || "date";
    return `<div class="field ui-date-field"><label for="${escapeHTML(id)}">${escapeHTML(options.label || "Дата")}${options.required ? " *" : ""}</label><input${attributes({
      id, name: options.name || id, type: "date", value: options.value ?? "", min: options.min,
      max: options.max, required: options.required, disabled: options.disabled,
    })}></div>`;
  }

  const riskLabels = { green: "Готово", yellow: "Нужна доработка", red: "Риск" };
  function riskBadge(options = {}) {
    const state = ["green", "yellow", "red"].includes(options.state) ? options.state : "red";
    const reasons = Array.isArray(options.reasons) ? options.reasons.filter(Boolean).join("; ") : options.reasons;
    return `<span class="risk-label ui-risk-badge ${state}"${attributes({ title: reasons || undefined, "data-risk": state })}><i aria-hidden="true"></i>${escapeHTML(options.label || riskLabels[state])}</span>`;
  }

  const iconRegistry = Object.freeze({
    "dashboard-target": '<circle cx="12" cy="12" r="7.5"></circle><circle cx="12" cy="12" r="3.5"></circle><path d="M12 12 20 4m-3 0h3v3"></path>',
    "dashboard-confirmed": '<path d="M7.5 11.5 10.5 14.5 17 8"></path><circle cx="12" cy="12" r="9"></circle>',
    "dashboard-gap": '<path d="m3 6 5 5 4-4 7 7"></path><path d="M15 14h4v-4"></path>',
    "dashboard-date": '<rect x="3" y="5" width="18" height="16" rx="2"></rect><path d="M8 3v4m8-4v4M3 10h18M7 14h2m3 0h2m3 0h1M7 17h2m3 0h2"></path>',
    dashboard: '<rect x="3" y="3" width="7" height="7" rx="1"></rect><rect x="14" y="3" width="7" height="7" rx="1"></rect><rect x="3" y="14" width="7" height="7" rx="1"></rect><rect x="14" y="14" width="7" height="7" rx="1"></rect>',
    entries: '<path d="M9 5h10a2 2 0 0 1 2 2v13a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V7"></path><path d="M9 3h6v4H9zM9 12h8M9 16h8"></path>',
    partners: '<path d="M3 21h18M5 21V8l7-4 7 4v13M9 12h2m2 0h2m-6 4h2m2 0h2"></path>',
    companies: '<path d="M4 21V7h9v14M13 11h7v10M7 10h2m-2 4h2m-2 4h2m9-3h-2m2 3h-2"></path>',
    admin: '<circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3v-.2h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"></path>',
    teachers: '<circle cx="9" cy="8" r="3"></circle><path d="M3.5 20v-2.5A4.5 4.5 0 0 1 8 13h2a4.5 4.5 0 0 1 4.5 4.5V20M15 5h6v10h-4"></path>',
    documents: '<path d="M6 3h9l4 4v14H6zM14 3v5h5M9 12h7M9 16h7"></path>',
    internship: '<path d="M4 7h16v13H4zM8 7V4h8v3M4 12h16M10 12v2h4v-2"></path>',
    practice: '<path d="M12 3 4 7v5c0 5 3.4 8.2 8 9.8 4.6-1.6 8-4.8 8-9.8V7zM8.5 12l2.2 2.2 4.8-5"></path>',
    top: '<path d="M4 20h16M6 17l4-5 3 2 5-7M15 7h3v3"></path>',
    schools: '<path d="m3 9 9-5 9 5-9 5zM6 11v6c3 2 9 2 12 0v-6M21 9v7"></path>',
    ministry: '<path d="m3 9 9-5 9 5M5 10h14M6 10v8m4-8v8m4-8v8m4-8v8M3 20h18"></path>',
    reports: '<path d="M5 3h14v18H5zM9 16v-3m3 3V8m3 8v-5M8 6h8"></path>',
    settings: '<circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3v-.2h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"></path>',
    download: '<path d="M12 3v11m0 0 4-4m-4 4-4-4M5 21h14"></path>',
    edit: '<path d="M4 20h4l10.5-10.5a2.1 2.1 0 0 0-3-3L5 17v3Z"></path>',
    close: '<path d="m6 6 12 12M18 6 6 18"></path>',
  });

  function icon(name, options = {}) {
    const path = iconRegistry[name] || iconRegistry.entries;
    const title = options.title ? `<title>${escapeHTML(options.title)}</title>` : "";
    return `<svg${attributes({
      class: options.className || undefined,
      viewBox: "0 0 24 24",
      "aria-hidden": options.title ? undefined : "true",
      "aria-label": options.title || undefined,
      focusable: "false",
    })}>${title}${path}</svg>`;
  }

  function filters(options = {}) {
    const fields = (options.fields || []).map((field) => {
      const id = field.id || field.name;
      const common = { id, name: field.name || id, disabled: field.disabled, "aria-label": field.label };
      let control;
      if (field.type === "select") {
        control = `<select${attributes(common)}>${(field.options || []).map((option) => {
          const value = typeof option === "object" ? option.value : option;
          const label = typeof option === "object" ? option.label : option;
          return `<option value="${escapeHTML(value)}"${String(value) === String(field.value ?? "") ? " selected" : ""}>${escapeHTML(label)}</option>`;
        }).join("")}</select>`;
      } else {
        control = `<input${attributes({ ...common, type: field.type || "search", value: field.value ?? "", placeholder: field.placeholder })}>`;
      }
      return `<div class="field"><label for="${escapeHTML(id)}">${escapeHTML(field.label)}</label>${control}</div>`;
    }).join("");
    return `<fieldset class="ui-filters"><legend>${escapeHTML(options.legend || "Фильтры")}</legend><div class="grid cols-${options.columns === 2 ? 2 : 3}">${fields}</div></fieldset>`;
  }

  function toggle(options = {}) {
    const id = options.id || options.name || "toggle";
    const checked = Boolean(options.checked);
    return `<div class="ui-toggle"><input${attributes({
      id, name: options.name || id, type: "checkbox", role: "switch", checked,
      disabled: options.disabled, "aria-describedby": options.hint ? `${id}-hint` : undefined,
    })}><label for="${escapeHTML(id)}"><span class="ui-toggle-track" aria-hidden="true"><span></span></span><span>${escapeHTML(options.label || "Переключатель")}</span></label>${options.hint ? `<div class="field-hint" id="${escapeHTML(id)}-hint">${escapeHTML(options.hint)}</div>` : ""}</div>`;
  }

  function inlineActions(options = {}) {
    return `<div class="ui-inline-actions" role="group"${attributes({ "aria-label": options.label || "Действия" })}>${(options.actions || []).map((action) => {
      const iconHTML = action.icon ? icon(action.icon) : "";
      if (action.href) {
        return `<a class="ui-inline-action"${attributes({ href: action.href, "data-action": action.name, "aria-disabled": action.disabled ? "true" : undefined, tabindex: action.disabled ? "-1" : undefined })}>${iconHTML}<span>${escapeHTML(action.label || action.name || "Действие")}</span></a>`;
      }
      return `<button type="button" class="ui-inline-action"${attributes({ "data-action": action.name, disabled: action.disabled, "aria-label": action.ariaLabel || action.label })}>${iconHTML}<span>${escapeHTML(action.label || action.name || "Действие")}</span></button>`;
    }).join("")}</div>`;
  }

  function compoundField(options = {}) {
    const id = options.id || options.name || "compound";
    const label = escapeHTML(options.label || "Составное поле");
    return `<fieldset class="ui-compound-field"${attributes({ "aria-describedby": options.hint ? `${id}-hint` : undefined })}><legend>${label}${options.required ? " *" : ""}</legend><div class="ui-compound-parts">${(options.parts || []).map((part, index) => {
      const partID = part.id || `${id}-${part.name || index}`;
      const labelHTML = part.label ? `<label for="${escapeHTML(partID)}">${escapeHTML(part.label)}</label>` : "";
      if (part.type === "select") {
        return `<div class="field">${labelHTML}<select${attributes({ id: partID, name: part.name || partID, required: part.required, disabled: part.disabled })}>${(part.options || []).map((item) => {
          const value = typeof item === "object" ? item.value : item;
          const itemLabel = typeof item === "object" ? item.label : item;
          return `<option value="${escapeHTML(value)}"${String(value) === String(part.value ?? "") ? " selected" : ""}>${escapeHTML(itemLabel)}</option>`;
        }).join("")}</select></div>`;
      }
      return `<div class="field">${labelHTML}<input${attributes({ id: partID, name: part.name || partID, type: part.type || "text", value: part.value ?? "", placeholder: part.placeholder, required: part.required, disabled: part.disabled })}></div>`;
    }).join("")}</div>${options.hint ? `<div class="field-hint" id="${escapeHTML(id)}-hint">${escapeHTML(options.hint)}</div>` : ""}</fieldset>`;
  }

  function validationSummary(options = {}) {
    const errors = (options.errors || []).filter(Boolean);
    const warnings = (options.warnings || []).filter(Boolean);
    if (!errors.length && !warnings.length) return "";
    return `<div class="ui-validation-summary ${errors.length ? "error" : "warning"}"${attributes({ role: errors.length ? "alert" : "status", tabindex: "-1" })}><strong>${escapeHTML(errors.length ? options.errorTitle || "Исправьте ошибки" : options.warningTitle || "Проверьте предупреждения")}</strong><ul>${errors.concat(warnings).map((item) => `<li>${escapeHTML(item)}</li>`).join("")}</ul></div>`;
  }

  function table(options = {}) {
    const columns = options.columns || [];
    const rows = options.rows || [];
    const rowKey = options.rowKey || "id";
    const heading = columns.map((column) => {
      const label = escapeHTML(column.label || column.key);
      if (!column.sortable) return `<th scope="col">${label}</th>`;
      const active = options.sort?.key === column.key;
      const direction = active ? options.sort.direction : "none";
      return `<th scope="col" aria-sort="${direction === "asc" ? "ascending" : direction === "desc" ? "descending" : "none"}"><button type="button" class="ui-table-sort" data-sort="${escapeHTML(column.key)}">${label}${active ? `<span aria-hidden="true">${direction === "desc" ? " ↓" : " ↑"}</span>` : ""}</button></th>`;
    }).join("");
    const body = rows.map((row) => {
      const key = String(row[rowKey] ?? "");
      const selected = key !== "" && key === String(options.selectedKey ?? "");
      const cells = columns.map((column) => {
        const value = column.render ? column.render(row) : escapeHTML(row[column.key] ?? "—");
        return `<td>${value}</td>`;
      }).join("");
      return `<tr data-row-key="${escapeHTML(key)}"${selected ? ' class="selected" aria-selected="true"' : ""}>${cells}</tr>`;
    }).join("");
    const empty = `<tr><td colspan="${Math.max(1, columns.length)}" class="ui-table-empty">${escapeHTML(options.empty || "Нет данных")}</td></tr>`;
    const caption = options.caption ? `<caption>${escapeHTML(options.caption)}</caption>` : "";
    return `<div class="table-wrap ui-table"><table>${caption}<thead><tr>${heading}</tr></thead><tbody>${body || empty}</tbody></table></div>`;
  }

  function drawerMarkup(options = {}) {
    const id = options.id || "ui-drawer";
    const mode = options.mode === "view" ? "view" : "edit";
    return `<div class="ui-drawer-backdrop" data-drawer-backdrop><section class="ui-drawer${options.wide ? " ui-drawer-wide" : ""}" id="${escapeHTML(id)}" role="dialog" aria-modal="true" aria-labelledby="${escapeHTML(id)}-title" data-drawer-mode="${mode}" tabindex="-1"><header><h2 id="${escapeHTML(id)}-title">${escapeHTML(options.title || "Панель")}</h2><button type="button" class="btn secondary" data-drawer-close aria-label="Закрыть панель">Закрыть</button></header><div class="ui-drawer-body">${options.content || ""}</div></section></div>`;
  }

  function focusableElements(root) {
    return [...root.querySelectorAll('a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])')];
  }

  function trapFocus(event, root) {
    if (event.key !== "Tab") return;
    const focusable = focusableElements(root);
    if (!focusable.length) return;
    const current = focusable.indexOf(root.ownerDocument.activeElement);
    const next = event.shiftKey ? (current <= 0 ? focusable.length - 1 : current - 1) : (current + 1) % focusable.length;
    event.preventDefault();
    focusable[next].focus();
  }

  function openDrawer(options = {}) {
    const doc = options.document || global.document;
    if (!doc) throw new Error("document is required");
    const template = doc.createElement("template");
    template.innerHTML = drawerMarkup(options);
    const backdrop = template.content.firstElementChild;
    const drawer = backdrop.querySelector(".ui-drawer");
    const previous = doc.activeElement;
    const close = () => {
      if (options.canClose && options.canClose() === false) return;
      backdrop.remove();
      if (previous?.focus) previous.focus();
      options.onClose?.();
    };
    backdrop.querySelector("[data-drawer-close]").addEventListener("click", close);
    backdrop.addEventListener("click", (event) => { if (event.target === backdrop) close(); });
    backdrop.addEventListener("keydown", (event) => {
      if (event.key === "Escape") close();
      else trapFocus(event, drawer);
    });
    doc.body.appendChild(backdrop);
    drawer.focus();
    return { element: backdrop, close };
  }

  function uploadField(options = {}) {
    const id = options.id || options.name || "upload-file";
    return `<div class="field ui-upload" data-upload><label for="${escapeHTML(id)}">${escapeHTML(options.label || "Файл")}${options.required ? " *" : ""}</label><div class="ui-dropzone" tabindex="0" role="button" aria-controls="${escapeHTML(id)}"><span>${escapeHTML(options.prompt || "Перетащите файл сюда или выберите с диска")}</span><input${attributes({ id, name: options.name || "file", type: "file", accept: options.accept, multiple: options.multiple, required: options.required, disabled: options.disabled })}></div>${options.hint ? `<div class="field-hint">${escapeHTML(options.hint)}</div>` : ""}</div>`;
  }

  function bindUpload(root, onFiles) {
    const input = root.querySelector('input[type="file"]');
    const zone = root.querySelector(".ui-dropzone") || root;
    if (!input) throw new Error("upload input is required");
    let selectedFiles = [];
    const emit = (files) => {
      selectedFiles = Array.from(files || []);
      onFiles?.(selectedFiles, input);
    };
    zone.addEventListener("click", (event) => { if (event.target !== input) input.click(); });
    zone.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") { event.preventDefault(); input.click(); }
    });
    for (const type of ["dragenter", "dragover"]) zone.addEventListener(type, (event) => { event.preventDefault(); zone.classList.add("drag-over"); });
    for (const type of ["dragleave", "drop"]) zone.addEventListener(type, (event) => { event.preventDefault(); zone.classList.remove("drag-over"); });
    zone.addEventListener("drop", (event) => emit(event.dataTransfer?.files));
    input.addEventListener("change", () => emit(input.files));
    return {
      input,
      zone,
      get files() { return selectedFiles.length ? selectedFiles : Array.from(input.files || []); },
      clear() { selectedFiles = []; input.value = ""; },
    };
  }

  function moveItem(items, from, to) {
    const copy = [...items];
    if (!Number.isInteger(from) || !Number.isInteger(to) || from < 0 || to < 0 || from >= copy.length || to >= copy.length || from === to) return copy;
    const [item] = copy.splice(from, 1);
    copy.splice(to, 0, item);
    return copy;
  }

  function bindReorder(root, onMove) {
    let dragged = null;
    const rows = () => [...root.querySelectorAll("[data-reorder-key]")];
    const move = (from, to) => {
      if (from === to || from < 0 || to < 0) return;
      onMove?.(from, to, rows()[from]?.dataset.reorderKey);
    };
    root.addEventListener("click", (event) => {
      const button = event.target.closest("[data-move]");
      if (!button) return;
      const row = button.closest("[data-reorder-key]");
      const from = rows().indexOf(row);
      move(from, button.dataset.move === "up" ? from - 1 : from + 1);
    });
    root.addEventListener("keydown", (event) => {
      if (!event.altKey || !["ArrowUp", "ArrowDown"].includes(event.key)) return;
      const row = event.target.closest("[data-reorder-key]");
      if (!row) return;
      event.preventDefault();
      const from = rows().indexOf(row);
      move(from, event.key === "ArrowUp" ? from - 1 : from + 1);
    });
    root.addEventListener("dragstart", (event) => { dragged = event.target.closest("[data-reorder-key]"); dragged?.setAttribute("aria-grabbed", "true"); });
    root.addEventListener("dragover", (event) => { if (event.target.closest("[data-reorder-key]")) event.preventDefault(); });
    root.addEventListener("drop", (event) => { event.preventDefault(); move(rows().indexOf(dragged), rows().indexOf(event.target.closest("[data-reorder-key]"))); });
    root.addEventListener("dragend", () => { dragged?.setAttribute("aria-grabbed", "false"); dragged = null; });
  }

  function toast(message, kind = "error", options = {}) {
    const doc = options.document || global.document;
    if (!doc) return null;
    const item = doc.createElement("div");
    item.className = `toast ${kind === "success" ? "success" : "error"}`;
    item.setAttribute("role", kind === "error" ? "alert" : "status");
    item.textContent = String(message ?? "");
    doc.body.appendChild(item);
    const frame = options.requestAnimationFrame || global.requestAnimationFrame || ((callback) => callback());
    const timer = options.setTimeout || global.setTimeout;
    frame(() => item.classList.add("visible"));
    timer(() => { item.classList.remove("visible"); timer(() => item.remove(), 200); }, options.duration ?? 3500);
    return item;
  }

  global.CyberCalcUI = Object.freeze({
    escapeHTML, formatMoney, moneyInput, parseMoney, dateInput, riskBadge, icon, iconRegistry,
    filters, toggle, inlineActions, compoundField, validationSummary, table,
    drawerMarkup, trapFocus, openDrawer, uploadField, bindUpload, moveItem, bindReorder, toast,
  });
})(globalThis);
