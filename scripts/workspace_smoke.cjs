// Run after isolated compose startup; PLAYWRIGHT_MODULE can point to an existing installation.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const assert = require("node:assert/strict");
const base = process.env.TEST_BASE_URL || "http://127.0.0.1:18081";
if (!["127.0.0.1", "localhost"].includes(new URL(base).hostname))
  throw new Error("Local test server only");

(async () => {
  const browser = await chromium.launch({
    headless: true,
    ...(process.env.CHROME_EXECUTABLE
      ? { executablePath: process.env.CHROME_EXECUTABLE }
      : {}),
  });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1050 },
  });
  const request = context.request;
  const post = async (path, data) => {
    const res = await request.post(base + "/api" + path, { data });
    assert.ok(res.ok(), `${path}: ${res.status()} ${await res.text()}`);
    return res.json();
  };
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  try {
    await post("/auth/login", {
      email: "admin@workspace.test",
      password: "WorkspaceTest1!",
    });
    const suffix = Date.now();
    const partner = await post("/partners", {
      name: "Браузерный тест ВУЗ " + suffix,
      partner_kind: "vuz",
      agreement_date: "2026-01-01",
      agreement_number: "Тест-01",
    });
    const mentor = await post("/mentors", {
      partner_id: partner.id,
      full_name: "Иванов Иван Иванович",
    });
    await page.goto(base);
    await page.locator('nav [data-view="entries"]').click();
    await page.locator("#workspace-partner").selectOption(partner.id);
    await page.locator("#category").selectOption("internship");
    await page.locator("#add-entry").click();
    await page.locator('[data-key="mentor_id"]').selectOption(mentor.id);
    await page
      .locator('[data-key="student_full_name"]')
      .fill("Петров Пётр Петрович");
    await page.locator('[data-key="duration_months"]').fill("2");
    await page.locator('[data-key="student_load_hours_per_month"]').fill("10");
    await page.locator('[data-key="mentor_load_hours_per_month"]').fill("3");
    await page.locator("#m-save").click();
    await page.locator(".modal-backdrop").waitFor({ state: "detached" });
    await page.getByText("Петров Пётр Петрович", { exact: true }).waitFor();
    await page.locator('[data-p="fact"]').click();
    await page.locator("#category").selectOption("teachers");
    await page.locator("#add-entry").click();
    await page.locator('[data-key="course_name"]').fill("Безопасность");
    await page.locator('[data-key="teacher_full_name"]').fill("Сидоров Сидор");
    await page.locator('[data-key="employment_form"]').selectOption("ГПХ");
    await page.locator('[data-key="academic_hours"]').fill("2");
    await page.locator("#attach-file").setInputFiles([
      { name: "Акт 1.txt", mimeType: "text/plain", buffer: Buffer.from("one") },
      { name: "Акт 2.txt", mimeType: "text/plain", buffer: Buffer.from("two") },
    ]);
    await page.locator("#m-save").click();
    await page.locator(".modal-backdrop").waitFor({ state: "detached" });
    await page.locator("[data-edit]").first().click();
    await page.locator("#attach-list a").first().waitFor();
    assert.equal(await page.locator("#attach-list a").count(), 2);
    await page.locator('[data-key="academic_hours"]').fill("3");
    await page.locator("#m-comment").fill("Уточнение часов");
    await page.locator("#m-save").click();
    await page.locator(".modal-backdrop").waitFor({ state: "detached" });
    await page.locator("#category").selectOption("top_it");
    await page
      .getByRole("heading", {
        name: "ТОП ИТ: обязательность будет снята после сохранения",
      })
      .waitFor();
    await page.locator("#add-entry").click();
    for (const [key, value] of Object.entries({
      project_name: "ТОП ИТ",
      program_name: "Тестовая программа",
      cofinancing_report_reference: "Отчёт 01",
      cofinancing_amount_rub: "1000",
    }))
      await page.locator(`[data-key="${key}"]`).fill(value);
    await page.locator("#m-save").click();
    await page.locator(".modal-backdrop").waitFor({ state: "detached" });
    await page
      .getByRole("heading", {
        name: "ТОП ИТ: обязательность снята",
        exact: true,
      })
      .waitFor();
    await page.locator("#category").selectOption("teachers");
    await page
      .getByRole("heading", {
        name: "ТОП ИТ: обязательность снята",
        exact: true,
      })
      .waitFor();
    await page.locator('[data-p="plan"]').click();
    await page
      .getByRole("heading", {
        name: "Обязательные активности вуза",
        exact: true,
      })
      .waitFor();
    await page.locator("#import-entries").click();
    await page
      .locator("#import-file")
      .setInputFiles({
        name: "bad.xlsx",
        mimeType: "application/octet-stream",
        buffer: Buffer.from("bad"),
      });
    await page.locator("#preview").click();
    await page
      .getByText("нужен файл .xlsx (не .xls и не CSV)", { exact: true })
      .waitFor();
    assert.equal(await page.locator("#commit").isDisabled(), true);
    await page.locator("#close-import").click();
    await page.screenshot({
      path: "/private/tmp/cybercalc-workspace-desktop.png",
      fullPage: true,
    });
    await page.locator('nav [data-view="partners"]').click();
    await page.locator("#d-search").fill("Баумана");
    await page.locator("#d-find").click();
    await page.locator("[data-directory]").first().waitFor();
    await page.locator("[data-directory]").first().click();
    assert.ok((await page.locator("#p-name").inputValue()).includes("Баумана"));
    await page.locator('nav [data-view="dashboard"]').click();
    await page.locator("#dash-partner").selectOption(partner.id);
    await page.locator("#dash-year").waitFor();
    await page.screenshot({
      path: "/private/tmp/cybercalc-workspace-dashboard.png",
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.locator('nav [data-view="entries"]').click();
    await page.locator("#category").waitFor();
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      "mobile page overflows horizontally",
    );
    await page.screenshot({
      path: "/private/tmp/cybercalc-workspace-mobile.png",
      fullPage: true,
    });
    assert.deepEqual(errors, []);
    console.log(
      "PASS: partner-first entry, mandatory mentor, no attachments, two attachments, edit, TOP exemption scoped to period, bad Excel preview, directory selection, dashboard, mobile rendering; no JS errors.",
    );
  } catch (e) {
    await page.screenshot({
      path: "/private/tmp/cybercalc-workspace-failure.png",
      fullPage: true,
    });
    throw e;
  } finally {
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
