// Enrollment does not store secrets in localStorage or include them in URLs.
function renderMFASetup() {
  const root = el(`<div class="auth-shell compact-auth">
    <aside class="auth-brand-panel">${brandMarkup(true)}<div class="auth-orbit" aria-hidden="true"><i></i><i></i><i></i></div></aside>
    <main class="auth-form-panel"><section class="login-box mfa-box"><span class="eyebrow">Безопасность</span><h1>Защита входа</h1>
      <form id="mfa-start"><div class="setup-step"><b>1</b><div><label>Подтвердите пароль<input type="password" autocomplete="current-password" maxlength="128" required></label></div></div><button class="btn wide">Продолжить</button></form>
      <div id="mfa-secret" hidden><div class="setup-step"><b>2</b><div><label>Добавьте ключ в приложение-аутентификатор</label><code></code><div class="field-hint">TOTP · 6 цифр · 30 секунд</div></div></div>
      <form id="mfa-confirm"><div class="setup-step"><b>3</b><div><label>Код из приложения<input inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required placeholder="000000"></label></div></div><button class="btn wide">Подтвердить</button></form></div>
      <div id="mfa-recovery" hidden><h2>Резервные коды</h2><p>Сохраните коды сейчас. Каждый код можно использовать один раз.</p><pre></pre><button class="btn wide" id="mfa-done">Готово</button></div>
      <p role="alert" class="error"></p><button class="btn secondary wide" id="mfa-exit">Выйти</button>
    </section></main>
  </div>`);
  const error = root.querySelector('[role="alert"]');
  root.querySelector("#mfa-start").onsubmit = async (event) => {
    event.preventDefault(); const form = event.currentTarget; const button = form.querySelector("button"); button.disabled = true;
    try { const result = await api("/auth/mfa/enroll", {method:"POST", body:JSON.stringify({password:form.querySelector("input").value})}); form.reset(); form.hidden = true; root.querySelector("#mfa-secret").hidden = false; root.querySelector("code").textContent = result.secret; error.textContent = ""; }
    catch (e) { error.textContent = e.message; } finally { button.disabled = false; }
  };
  root.querySelector("#mfa-confirm").onsubmit = async (event) => {
    event.preventDefault(); const form = event.currentTarget; const button = form.querySelector("button"); button.disabled = true;
    try { const result = await api("/auth/mfa/confirm", {method:"POST", body:JSON.stringify({code:form.querySelector("input").value})}); root.querySelector("code").textContent = ""; root.querySelector("#mfa-secret").hidden = true; root.querySelector("#mfa-recovery").hidden = false; root.querySelector("pre").textContent = result.recovery_codes.join("\n"); root.querySelector("#mfa-exit").hidden = true; error.textContent = ""; state.me = null; }
    catch (e) { error.textContent = e.message; } finally { button.disabled = false; }
  };
  root.querySelector("#mfa-done").onclick = () => { root.querySelector("pre").textContent = ""; render(); };
  root.querySelector("#mfa-exit").onclick = async () => { try { await api("/auth/logout", {method:"POST"}); state.me = null; render(); } catch(e) { error.textContent=e.message; } };
  return root;
}
