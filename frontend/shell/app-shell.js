// Общая оболочка SPA. Предметные экраны подключаются через lazy loader и не
// изменяют sidebar/router напрямую.
function renderLayout() {
  const profileLabel =
    state.me.entity_type === "organization"
      ? "ИТ-организация"
      : "Учебное заведение";
  const screens = CyberCalcScreens.all;
  if (!CyberCalcScreens.get(state.view)) state.view = "dashboard";
  const activeScreen = CyberCalcScreens.get(state.view);
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
          ${screens.map((screen) => `<button data-view="${screen.id}" title="Экран ${screen.number}. ${escapeHTML(screen.label)}">${navigationIcon(screen.icon)}<span><small>${screen.number}</small>${escapeHTML(screen.label)}</span>${screen.id === "partners" ? '<span class="nav-count" data-directory-proposal-count aria-live="polite" hidden></span>' : ""}</button>`).join("")}
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
