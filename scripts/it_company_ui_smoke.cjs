// Browser regression checks with isolated API fixtures; no real account or registry writes.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const frontend = path.resolve(__dirname, '../frontend');
const companies = Array.from({ length: 650 }, (_, i) => ({
  name: `Компания ${i}`, inn: String(7700000000 + i), ogrn: '1027700132195',
  accreditation_status: 'active', registry_updated_at: '2026-09-15',
  source_url: 'https://www.gosuslugi.ru/itorgs', notes: '',
}));
(async () => {
  const server = http.createServer((req, res) => {
    const filename = new URL(req.url, 'http://localhost').pathname;
    const file = path.join(frontend, filename === '/' ? 'index.html' : path.basename(filename));
    res.setHeader('Content-Type', file.endsWith('.js') ? 'application/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html');
    fs.createReadStream(file).on('error', () => { res.statusCode = 404; res.end(); }).pipe(res);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  let browser;
  try {
    browser = await chromium.launch({ headless: true, ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}) });
    const page = await browser.newPage();
    const errors = [];
    const searches = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.route('**/api/**', async route => {
      const url = new URL(route.request().url());
      let body = [], status = 200, headers = {};
      if (url.pathname === '/api/auth/me') { status = 401; body = { error: 'test login' }; }
      else if (url.pathname === '/api/dashboard') { status = 500; body = { error: 'unused fixture' }; }
      else if (url.pathname === '/api/directory/stats') body = {};
      else if (url.pathname === '/api/it-companies/registry-search') {
        searches.push(url.searchParams.get('q'));
        status = 502; body = { error: 'Сервис проверки реестра временно недоступен' };
      } else if (url.pathname === '/api/it-companies') {
        const q = url.searchParams.get('q') || '';
        const filtered = companies.filter(c => c.name.includes(q) || c.inn === q);
        const offset = Number(url.searchParams.get('offset') || 0);
        body = filtered.slice(offset, offset + 500);
        if (filtered.length > offset + 500) headers['X-Next-Offset'] = String(offset + 500);
      }
      await route.fulfill({ status, contentType: 'application/json', headers, body: JSON.stringify(body) });
    });
    await page.goto(`http://127.0.0.1:${server.address().port}`);
    await page.locator('#login-email').waitFor();
    await page.evaluate(() => { state.me = { role: 'user', entity_type: 'edu_institution', partner_id: 'test-partner', full_name: 'Тест ОО' }; state.view = 'it-companies'; render(); });
    await page.locator('#it-list tbody tr').first().waitFor();
    assert.equal(await page.locator('nav [data-view="it-companies"]').count(), 1);
    assert.equal(await page.locator('#it-list tbody tr').count(), 500);
    assert.equal(await page.locator('#it-add').count(), 0);
    assert.equal(await page.getByText('Загрузить выгрузку', { exact: true }).count(), 0);
    assert.deepEqual(searches, []);
    await page.locator('[data-next]').click();
    await page.waitForFunction(() => document.querySelector('#it-count').textContent.includes('501–650'));
    assert.equal(await page.locator('#it-list tbody tr').count(), 150);
    await page.locator('#it-search').fill('7700000001');
    await page.locator('#it-find').click();
    await page.waitForFunction(() => document.querySelectorAll('#it-list tbody tr').length === 1);
    await page.locator('#it-search').fill('');
    await page.locator('#it-scope').selectOption('registry');
    await page.getByText('Введите название или ИНН и нажмите «Найти».', { exact: true }).waitFor();
    assert.deepEqual(searches, []);
    await page.locator('#it-search').fill('Киберпротект');
    await page.locator('#it-find').click();
    await page.locator('[data-it-saved]').waitFor();
    await page.locator('[data-it-saved]').click();
    await page.waitForFunction(() => document.querySelectorAll('#it-list tbody tr').length === 500);
    assert.deepEqual(searches, ['Киберпротект']);
    for (const role of ['admin', 'moderator']) {
      await page.evaluate(role => { state.me.role = role; state.view = 'admin'; render(); }, role);
      await page.locator('.admin-nav [data-t="it-companies"]').click();
      await page.locator('#it-add').waitFor();
      await page.locator('#it-list tbody tr').first().waitFor();
    }
    await page.evaluate(() => { state.me.role = 'moderator'; state.me.entity_type = 'organization'; state.view = 'admin'; render(); });
    assert.equal(await page.locator('.admin-nav [data-t="it-companies"]').count(), 0);
    assert.equal(await page.locator('.admin-nav [data-t="partners"]').count(), 1);
    await page.evaluate(() => renderAdminTab(document.querySelector('#admin-content'), 'it-companies'));
    await page.getByText('Реестр ИТ-компаний недоступен для этого профиля', { exact: true }).waitFor();
    assert.deepEqual(errors, []);
    console.log('PASS: education read access, manager controls, IT moderator visibility, 650-row pagination, INN filter, registry outage recovery');
  } finally {
    if (browser) await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
