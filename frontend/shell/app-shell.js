// Общая оболочка SPA. Предметные экраны подключаются через lazy loader и не
// изменяют sidebar/router напрямую.
function screenAccess(screen, user) {
  const role = user?.role || "";
  const entityType = user?.entity_type || "";
  const itCompanyOperator = entityType === "organization";
  const canAuthorReports = itCompanyOperator && ["super_admin", "holding_admin", "org_admin", "curator"].includes(role);
  const canAdmin = itCompanyOperator && ["super_admin", "holding_admin", "org_admin"].includes(role);
  const categories = Array.from(screen?.categoryCodes || []);
  if (categories.length) {
    const writable =
      canAuthorReports ||
      (itCompanyOperator && role === "hr_specialist" && categories.every((category) => ["internship", "employment_practice"].includes(category))) ||
      (itCompanyOperator && role === "financial_specialist" && categories.every((category) => category === "teachers"));
    return writable
      ? { mode: "write", label: "доступно редактирование" }
      : { mode: "read", label: "только просмотр по роли" };
  }
  if (screen?.id === "reports") {
    return canAuthorReports
      ? { mode: "write", label: "формирование и выгрузка отчётов" }
      : { mode: "read", label: "просмотр доступной отчётности" };
  }
  if (screen?.id === "settings") {
    return canAdmin
      ? { mode: "admin", label: "администрирование доступно" }
      : { mode: "read", label: "настройки в режиме просмотра" };
  }
  if (screen?.id === "partners") {
    return (canAuthorReports || (entityType === "edu_institution" && role === "curator"))
      ? { mode: "write", label: "ведение партнёров по роли" }
      : { mode: "read", label: "просмотр партнёров" };
  }
  return { mode: "read", label: "просмотр раздела" };
}

function focusNavButton(buttons, index) {
  const target = buttons[index];
  if (!target) return;
  target.focus();
  target.scrollIntoView?.({ block: "nearest" });
}

function bindPrimaryNavKeyboard(nav) {
  const buttons = Array.from(nav.querySelectorAll("button[data-view]"));
  nav.addEventListener("keydown", (event) => {
    const current = event.target.closest?.("button[data-view]");
    if (!current) return;
    const index = buttons.indexOf(current);
    if (index < 0) return;
    if (event.key === "ArrowDown" || event.key === "ArrowRight") {
      event.preventDefault();
      focusNavButton(buttons, (index + 1) % buttons.length);
    } else if (event.key === "ArrowUp" || event.key === "ArrowLeft") {
      event.preventDefault();
      focusNavButton(buttons, (index - 1 + buttons.length) % buttons.length);
    } else if (event.key === "Home") {
      event.preventDefault();
      focusNavButton(buttons, 0);
    } else if (event.key === "End") {
      event.preventDefault();
      focusNavButton(buttons, buttons.length - 1);
    }
  });
}

function renderLayout() {
  const profileLabel =
    state.me.entity_type === "organization"
      ? "ИТ-организация"
      : "Учебное заведение";
  const screens = CyberCalcScreens.available();
  if (!screens.length) {
    const empty = el(`<main class="system-state"><div class="card"><span class="state-mark" aria-hidden="true">!</span><h1>Модули не включены</h1><p>Администратор appliance должен включить разрешённые экраны в конфигурации feature flags.</p><button class="btn secondary" id="no-feature-logout">Выйти</button></div></main>`);
    empty.querySelector("#no-feature-logout").onclick = async () => {
      await api("/auth/logout", { method: "POST" });
      state.me = null;
      render();
    };
    return empty;
  }
  if (!CyberCalcScreens.getAvailable(state.view)) state.view = screens[0].id;
  const activeScreen = CyberCalcScreens.getAvailable(state.view);
  const organizationName = state.me.organization_name || state.me.entity_name || profileLabel;
  const wrap = el(`<div class="app-shell">
    <a class="skip-link" href="#content">К содержанию</a>
    <header class="topbar">
      <div class="topbar-brand">${brandMarkup(true)}</div>
      <button type="button" class="topbar-menu" id="sidebar-toggle" aria-label="Свернуть меню" aria-controls="primary-sidebar" aria-expanded="true"><span></span><span></span><span></span></button>
      <div class="product-context"><strong>${escapeHTML(activeScreen.title)}</strong><span>${escapeHTML(activeScreen.subtitle)}</span></div>
      <div class="who">
        <span class="user-copy"><strong>${escapeHTML(state.me.full_name)}</strong><small>${profileLabel} · ${escapeHTML(valueLabel(state.me.role))}</small></span>
        <span class="avatar">${escapeHTML(initials(state.me.full_name))}</span>
        <button class="header-action" id="change-password" title="Изменить пароль" aria-label="Изменить пароль"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 11h16v10H4zM8 11V7a4 4 0 0 1 8 0v4"></path></svg></button>
        ${state.me.mfa_available && !state.me.mfa_enabled ? '<button class="header-action header-action-text" id="setup-mfa" title="Настроить двухфакторную аутентификацию">2FA</button>' : ''}
        <button class="header-action" id="logout" title="Выйти из системы" aria-label="Выйти из системы"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10 5H4v14h6M14 8l4 4-4 4m4-4H9"></path></svg></button>
      </div>
    </header>
    <div class="app-body">
      <aside class="sidebar" id="primary-sidebar">
        <div class="tenant-card"><span>Рабочее пространство</span><strong title="${escapeHTML(organizationName)}">${escapeHTML(organizationName)}</strong><small>${escapeHTML(profileLabel)}</small></div>
        <nav class="primary-nav" aria-label="Основная навигация">
          <span class="nav-section-title">11 экранов системы</span>
          ${screens.map((screen) => {
            const access = screenAccess(screen, state.me);
            const label = `Экран ${screen.number}. ${screen.label}: ${access.label}`;
            return `<button data-view="${screen.id}" data-access-mode="${access.mode}" title="${escapeHTML(label)}" aria-label="${escapeHTML(label)}">${navigationIcon(screen.icon)}<span><small>${screen.number}</small>${escapeHTML(screen.label)}</span>${screen.id === "partners" ? '<span class="nav-count" data-directory-proposal-count aria-live="polite" hidden></span>' : ""}</button>`;
          }).join("")}
        </nav>
        <div class="sidebar-footer"><span class="system-indicator"></span><div><b>Система доступна</b><small>Защищённое соединение</small></div></div>
      </aside>
      <main class="container" id="content"><div class="loading-state"><span class="spinner"></span>Загрузка раздела…</div></main>
    </div>
  </div>`);
  wrap.querySelectorAll(".primary-nav button").forEach((button) => {
    if (button.dataset.view === state.view) {
      button.classList.add("active");
      button.setAttribute("aria-current", "page");
    }
    button.onclick = () => CyberCalcRouter.activate(button.dataset.view);
  });
  bindPrimaryNavKeyboard(wrap.querySelector(".primary-nav"));
  wrap.querySelector("#sidebar-toggle").onclick = () => {
    if (window.matchMedia("(max-width: 680px)").matches) {
      wrap.classList.remove("sidebar-collapsed");
      const opened = wrap.classList.toggle("sidebar-mobile-open");
      wrap.querySelector("#sidebar-toggle").setAttribute("aria-expanded", String(opened));
      wrap.querySelector("#sidebar-toggle").setAttribute("aria-label", opened ? "Закрыть меню" : "Открыть меню");
      return;
    }
    wrap.classList.remove("sidebar-mobile-open");
    const collapsed = wrap.classList.toggle("sidebar-collapsed");
    wrap.querySelector("#sidebar-toggle").setAttribute("aria-expanded", String(!collapsed));
    wrap.querySelector("#sidebar-toggle").setAttribute("aria-label", collapsed ? "Развернуть меню" : "Свернуть меню");
  };
  if (window.matchMedia("(max-width: 680px)").matches) {
    wrap.querySelector("#sidebar-toggle").setAttribute("aria-expanded", "false");
    wrap.querySelector("#sidebar-toggle").setAttribute("aria-label", "Открыть меню");
  }
  wrap.addEventListener("click", (event) => {
    if (!wrap.classList.contains("sidebar-mobile-open")) return;
    if (event.target.closest("#primary-sidebar") || event.target.closest("#sidebar-toggle")) return;
    wrap.classList.remove("sidebar-mobile-open");
    wrap.querySelector("#sidebar-toggle").setAttribute("aria-expanded", "false");
    wrap.querySelector("#sidebar-toggle").setAttribute("aria-label", "Открыть меню");
  });
  wrap.querySelector("#logout").onclick = async () => {
    try {
      await api("/auth/logout", { method: "POST" });
      state.me = null;
      render();
    } catch (error) {
      showToast(error.message);
    }
  };
  wrap.querySelector("#change-password").onclick = openPasswordDialog;
  const setupMFA = wrap.querySelector("#setup-mfa");
  if (setupMFA) setupMFA.onclick = () => app.replaceChildren(renderMFASetup());

  const content = wrap.querySelector("#content");
  CyberCalcScreenLoader.render(state.view, content).catch((error) => {
    content.innerHTML = `<div class="card error">${escapeHTML(error.message)}</div>`;
    showToast(error.message);
  });
  refreshDirectoryProposalBadge(wrap);
  return wrap;
}
