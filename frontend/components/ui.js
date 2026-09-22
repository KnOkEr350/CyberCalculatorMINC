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
    return `<div class="ui-drawer-backdrop" data-drawer-backdrop><section class="ui-drawer" id="${escapeHTML(id)}" role="dialog" aria-modal="true" aria-labelledby="${escapeHTML(id)}-title" tabindex="-1"><header><h2 id="${escapeHTML(id)}-title">${escapeHTML(options.title || "Панель")}</h2><button type="button" class="btn secondary" data-drawer-close aria-label="Закрыть панель">Закрыть</button></header><div class="ui-drawer-body">${options.content || ""}</div></section></div>`;
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
    escapeHTML, formatMoney, moneyInput, parseMoney, dateInput, riskBadge, filters, table,
    drawerMarkup, trapFocus, openDrawer, uploadField, bindUpload, moveItem, bindReorder, toast,
  });
})(globalThis);
