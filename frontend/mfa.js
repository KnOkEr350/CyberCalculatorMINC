// Enrollment does not store secrets in localStorage or include them in URLs.
function renderMFASetup() {
  const root = el(`<main class="container"><section class="card"><h2>Двухфакторная защита</h2>
    <p>Для администратора production требуется приложение-аутентификатор. Добавьте в нём учётную запись по секретному ключу: TOTP, 6 цифр, интервал 30 секунд.</p>
    <form id="mfa-start"><label>Текущий пароль<input type="password" autocomplete="current-password" maxlength="128" required></label><button class="btn">Начать настройку</button></form>
    <div id="mfa-secret" hidden><p>Секретный ключ (не передавайте другим):</p><code></code>
    <form id="mfa-confirm"><label>Код из приложения<input inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required></label><button class="btn">Подтвердить</button></form></div>
    <div id="mfa-recovery" hidden><p>Сохраните резервные коды в менеджере паролей. Каждый используется один раз вместо кода приложения. Повторно эти коды не показываются.</p><pre></pre><button class="btn" id="mfa-done">Коды сохранены — перейти ко входу</button></div>
    <p role="alert"></p><button class="btn secondary" id="mfa-exit">Выйти</button></section></main>`);
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
