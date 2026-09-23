// DATA-05: versioned OKZ catalog administration and search screen.
(function initializeOKZ(global) {
  "use strict";

  function validCode(value) {
    return /^[0-9]{1,4}$/.test(String(value || ""));
  }

  function buildSearchPath({ query = "", level = "", limit = 50, offset = 0 } = {}) {
    const params = new URLSearchParams();
    if (String(query).trim()) params.set("q", String(query).trim());
    if (String(level).trim()) params.set("level", String(level).trim());
    params.set("limit", String(limit));
    params.set("offset", String(offset));
    return `/okz?${params.toString()}`;
  }

  async function render(root, dependencies) {
    const { api, escapeHTML, showToast } = dependencies;
    const today = new Date().toISOString().slice(0, 10);
    root.innerHTML = `<div class="section-intro"><div><h2>Справочник ОКЗ</h2></div></div>
      <div class="card">
        <div class="flex between"><div><h2>Активная версия</h2><p class="muted" id="okz-active-summary">Загрузка…</p></div><a class="btn secondary" href="/api/admin/okz/template">Скачать CSV-шаблон</a></div>
        <form id="okz-import" enctype="multipart/form-data">
          <div class="grid cols-3">
            <div class="field"><label for="okz-version">Обозначение версии *</label><input id="okz-version" name="version" maxlength="64" placeholder="2026.1" required></div>
            <div class="field"><label for="okz-effective">Действует с *</label><input id="okz-effective" name="effective_on" type="date" value="${today}" required></div>
            <div class="field"><label for="okz-source-name">Источник *</label><input id="okz-source-name" name="source_name" maxlength="300" value="Росстандарт — ОК 010-2014 (МСКЗ-08)" required></div>
            <div class="field"><label for="okz-source-url">Ссылка на источник</label><input id="okz-source-url" name="source_url" type="url" value="https://protect.gost.ru/classificators/details/0d7c57d3-084a-42e7-bdda-3c68bbc01731"></div>
            <div class="field"><label for="okz-file">CSV-файл *</label><input id="okz-file" name="file" type="file" accept=".csv,text/csv" required><div class="field-hint">Колонки code;name. Добавьте все уровни иерархии от 1 до 4 цифр.</div></div>
          </div>
          <p class="error" id="okz-import-error" role="alert"></p>
          <button type="submit" class="btn" id="okz-import-submit">Загрузить и сделать активной</button>
        </form>
      </div>
      <div class="card">
        <h2>Поиск по активному справочнику</h2>
        <form id="okz-search" class="okz-search-toolbar">
          <input name="query" aria-label="Код или наименование ОКЗ" placeholder="Код или наименование">
          <select name="level" aria-label="Уровень ОКЗ"><option value="">Все уровни</option><option value="1">1 — основная группа</option><option value="2">2 — подгруппа</option><option value="3">3 — малая группа</option><option value="4">4 — начальная группа</option></select>
          <button class="btn secondary" type="submit">Найти</button>
        </form>
        <div id="okz-search-result" aria-live="polite">Загрузка…</div>
      </div>
      <div class="card"><h2>История версий</h2><div id="okz-versions">Загрузка…</div></div>`;

    let currentOffset = 0;
    const pageSize = 50;
    const searchForm = root.querySelector("#okz-search");
    const result = root.querySelector("#okz-search-result");

    async function loadSearch(offset = 0) {
      currentOffset = Math.max(0, offset);
      result.innerHTML = '<div class="loading-state"><span class="spinner"></span>Поиск…</div>';
      let page;
      try {
        page = await api(buildSearchPath({
          query: searchForm.elements.query.value,
          level: searchForm.elements.level.value,
          limit: pageSize,
          offset: currentOffset,
        }));
      } catch (error) {
        result.innerHTML = `<div class="empty-state"><b>Справочник ОКЗ пока недоступен</b><span>${escapeHTML(error.message)}. Загрузите активную версию CSV или примените миграции справочника.</span></div>`;
        return;
      }
      if (!page.version) {
        result.innerHTML = '<div class="empty-state"><b>Справочник ещё не загружен</b><span>Загрузите первую версию CSV в форме выше.</span></div>';
        return;
      }
      const rows = page.items.map((item) => `<tr><td><code>${escapeHTML(item.code)}</code></td><td>${item.level}</td><td>${item.parent_code ? `<code>${escapeHTML(item.parent_code)}</code>` : "—"}</td><td>${escapeHTML(item.name)}</td></tr>`).join("");
      const from = page.total ? page.offset + 1 : 0;
      const to = Math.min(page.offset + page.items.length, page.total);
      result.innerHTML = `${page.items.length ? `<div class="table-wrap"><table><thead><tr><th>Код</th><th>Уровень</th><th>Родитель</th><th>Наименование</th></tr></thead><tbody>${rows}</tbody></table></div>` : '<p class="muted">Совпадений не найдено.</p>'}
        <div class="flex between okz-page-footer"><small>Версия ${escapeHTML(page.version)} · показано ${from}–${to} из ${page.total}</small><div class="flex"><button type="button" class="btn secondary" data-okz-prev ${page.offset === 0 ? "disabled" : ""}>Назад</button><button type="button" class="btn secondary" data-okz-next ${page.offset + page.items.length >= page.total ? "disabled" : ""}>Далее</button></div></div>`;
      result.querySelector("[data-okz-prev]").onclick = () => loadSearch(Math.max(0, currentOffset - pageSize)).catch((error) => showToast(error.message));
      result.querySelector("[data-okz-next]").onclick = () => loadSearch(currentOffset + pageSize).catch((error) => showToast(error.message));
    }

    async function loadVersions() {
      let versions;
      try {
        versions = await api("/admin/okz/versions");
      } catch (error) {
        root.querySelector("#okz-active-summary").textContent = "Версии ОКЗ недоступны";
        root.querySelector("#okz-versions").innerHTML = `<p class="muted">${escapeHTML(error.message)}. После применения миграций загрузите CSV-версию справочника.</p>`;
        return;
      }
      const active = versions.find((item) => item.status === "active");
      root.querySelector("#okz-active-summary").textContent = active
        ? `${active.version} · действует с ${active.effective_on} · ${active.item_count} записей`
        : "Активная версия отсутствует";
      root.querySelector("#okz-versions").innerHTML = versions.length
        ? `<div class="table-wrap"><table><thead><tr><th>Версия</th><th>Статус</th><th>Действует с</th><th>Записей</th><th>Источник</th><th>Загружена</th></tr></thead><tbody>${versions.map((item) => `<tr><td><b>${escapeHTML(item.version)}</b></td><td><span class="status-badge ${item.status === "active" ? "active" : "inactive"}">${item.status === "active" ? "Активна" : "Архив"}</span></td><td>${escapeHTML(item.effective_on)}</td><td>${item.item_count}</td><td>${item.source_url ? `<a href="${escapeHTML(item.source_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(item.source_name)}</a>` : escapeHTML(item.source_name)}</td><td>${new Date(item.imported_at).toLocaleString("ru-RU")}</td></tr>`).join("")}</tbody></table></div>`
        : '<p class="muted">Версий пока нет.</p>';
    }

    searchForm.onsubmit = (event) => {
      event.preventDefault();
      loadSearch(0).catch((error) => showToast(error.message));
    };
    root.querySelector("#okz-import").onsubmit = async (event) => {
      event.preventDefault();
      const form = event.currentTarget;
      const errorBox = root.querySelector("#okz-import-error");
      const submit = root.querySelector("#okz-import-submit");
      errorBox.textContent = "";
      submit.disabled = true;
      submit.textContent = "Проверяем и загружаем…";
      try {
        const uploaded = await api("/admin/okz/import", { method: "POST", body: new FormData(form) });
        showToast(`Версия ${uploaded.version} активирована: ${uploaded.item_count} записей`, "success");
        await render(root, dependencies);
      } catch (error) {
        errorBox.textContent = error.message;
      } finally {
        if (submit.isConnected) {
          submit.disabled = false;
          submit.textContent = "Загрузить и сделать активной";
        }
      }
    };

    await Promise.all([loadVersions(), loadSearch(0)]);
  }

  global.CyberCalcOKZ = Object.freeze({ buildSearchPath, render, validCode });
})(globalThis);
