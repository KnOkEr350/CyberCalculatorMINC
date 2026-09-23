# Трассировка задач плана ТЗ 4.4

Файл создаётся командой `go run ./cmd/traceability > ../docs/TRACEABILITY.md` (из каталога `backend`).
Связи берутся из упоминаний идентификаторов задач в коде, тестах и документах и из
`docs/traceability_evidence.json`. Матрица показывает, где искать исполнение задачи; результат
приёмки она не заменяет.

Задач: 202 — выполнено 151, частично 29, не начато 22.

| Задача | Название | Статус | Код | Тесты | Документы |
|---|---|---|---|---|---|
| ADR-07 | Срок хранения вложений и аудита | не начато | — | — | `docs/ORDER_270_ADR_ANALYSIS.md`<br>`docs/adr/ADR-07-retention-policy.md`<br>`docs/adr/README.md` |
| ADR-14 | Знаменатель процентов ТОП-ИТ | не начато | `backend/internal/compliance/readiness.go`<br>`backend/internal/handlers/ano_ac_report.go`<br>`backend/internal/handlers/top_items.go`<br>`backend/internal/topit/items.go` и ещё 3 | `backend/internal/handlers/top_program_integration_test.go`<br>`backend/internal/topit/scale_test.go`<br>`frontend/architecture.test.cjs` | `docs/ORDER_270_ADR_ANALYSIS.md`<br>`docs/adr/ADR-14-top-it-percent-denominator.md`<br>`docs/adr/README.md` |
| ADR-16 | Расчёт ИТ-стажа за последние пять лет | не начато | — | — | `docs/ORDER_270_ADR_ANALYSIS.md`<br>`docs/adr/ADR-16-it-experience.md`<br>`docs/adr/README.md` |
| ADR-20 | Snapshot cutoff и часовой пояс | не начато | — | — | `docs/ORDER_270_ADR_ANALYSIS.md`<br>`docs/adr/ADR-20-snapshot-cutoff.md`<br>`docs/adr/README.md` |
| ADR-21 | Допустимое название и эмблема продукта | не начато | — | — | `docs/ORDER_270_ADR_ANALYSIS.md`<br>`docs/UI_COMPONENTS.md`<br>`docs/adr/ADR-21-product-brand.md`<br>`docs/adr/README.md` |
| BASE-01 | Каталог архитектурных решений | выполнено | — | — | `docs/ORDER_270_ADR_ANALYSIS.md`<br>`docs/adr/README.md` |
| BASE-02 | Версионированный API-контракт | выполнено | `backend/internal/platform/httpx/response.go` | — | — |
| BASE-03 | Стандарт модулей backend | выполнено | `backend/internal/modules/health` | — | `backend/internal/modules/README.md` |
| BASE-04 | Модульная регистрация маршрутов | выполнено | `backend/internal/server/routes.go` | `backend/internal/server/composition_test.go` | — |
| BASE-05 | Разбиение frontend по экранам | выполнено | `frontend/core/router.js`<br>`frontend/core/store.js`<br>`frontend/shell/app-shell.js` | `frontend/architecture.test.cjs` | — |
| BASE-06 | Общая UI-библиотека | выполнено | `frontend/components/ui.js` | — | `docs/UI_COMPONENTS.md`<br>`docs/UI_COMPONENTS.md` |
| BASE-07 | Правила SQL-миграций | выполнено | `backend/cmd/migrationcheck/main.go`<br>`migrations/checksums.sha256` | `backend/internal/traceability/traceability_test.go` | `docs/MIGRATIONS.md` |
| BASE-08 | Feature flags | выполнено | `backend/internal/platform/featureflags`<br>`frontend/features.js` | `frontend/features.test.cjs` | — |
| BASE-09 | Общие тестовые фабрики | выполнено | `backend/internal/testfixtures/factory.go` | — | `docs/TEST_FIXTURES.md`<br>`docs/TEST_FIXTURES.md` |
| BASE-10 | Контракт событий аудита | выполнено | `migrations/0900_audit_event_contract.sql` | — | `docs/AUDIT_EVENTS.md`<br>`docs/LEGACY_MIGRATION.md` |
| BASE-11 | Стратегия legacy migration | выполнено | — | — | `docs/LEGACY_MIGRATION.md`<br>`docs/LEGACY_MIGRATION.md` |
| BASE-12 | CI-матрица модулей | выполнено | `.github/workflows/ci-cd.yml` | — | — |
| BASE-13 | Реестр нормативных источников | выполнено | `backend/internal/handlers/ano_ac_report.go`<br>`migrations/0103_normative_sources.sql`<br>`migrations/0204_organizations_and_specialties.sql` | — | `docs/NORMATIVE_SOURCES.md` |
| DATA-01 | Организации и партнёры v4.4 | выполнено | `migrations/0204_organizations_and_specialties.sql` | `backend/internal/traceability/traceability_test.go` | `docs/TZ_4_4_FOUNDATION_PACKAGES.md` |
| DATA-02 | Соглашения v4.4 | частично | `backend/internal/handlers/agreement_history.go`<br>`frontend/workspace.js`<br>`migrations/0112_agreement_history_and_curator.sql` | `backend/internal/handlers/agreement_history_integration_test.go`<br>`backend/internal/traceability/traceability_test.go`<br>`frontend/architecture.test.cjs` | — |
| DATA-03 | Структурные подразделения | выполнено | `migrations/0201_partner_academic_structure.sql` | — | — |
| DATA-04 | Академические группы | выполнено | `backend/internal/handlers/partner_structure.go` | — | — |
| DATA-05 | Справочник ОКЗ | выполнено | `frontend/okz.js`<br>`migrations/0200_okz_directory.sql` | — | `docs/OKZ_DIRECTORY.md` |
| DATA-06 | Специальности Приказа № 27 | выполнено | `migrations/0204_organizations_and_specialties.sql` | — | `docs/TZ_4_4_FOUNDATION_PACKAGES.md` |
| DATA-07 | Тарифы и формулы Приказа № 270 | выполнено | `backend/internal/calculators/exact.go`<br>`backend/internal/calculators/internship.go`<br>`backend/internal/calculators/ood_rpd.go`<br>`backend/internal/calculators/order_activities.go` и ещё 5 | `backend/internal/handlers/tariff_integration_test.go` | `docs/TZ_4_4_FOUNDATION_PACKAGES.md`<br>`docs/adr/ADR-24-no-gph-surcharge.md` |
| DATA-08 | Профили сотрудников | выполнено | `migrations/0300_teaching_staff_and_payouts.sql` | — | `docs/adr/ADR-16-it-experience.md` |
| DATA-09 | Кураторы и назначения | выполнено | `backend/internal/curators/curators.go`<br>`backend/internal/handlers/curator_assignments.go`<br>`backend/internal/rbac/rbac.go`<br>`migrations/0109_curator_assignments.sql` | `backend/internal/handlers/curator_assignments_integration_test.go`<br>`backend/internal/handlers/curator_scope_by_date_integration_test.go` | — |
| DATA-10 | Группы юридических лиц v4.4 | частично | `frontend/workspace.js`<br>`migrations/0111_legal_entity_groups_v44.sql` | `backend/internal/handlers/legal_entity_groups_v44_integration_test.go`<br>`frontend/architecture.test.cjs` | — |
| DATA-11 | 3% база и доведение Минцифры | выполнено | `backend/internal/handlers/budget_history.go`<br>`backend/internal/handlers/dashboard.go`<br>`migrations/0806_budget_basis_and_history.sql` | `backend/internal/handlers/budget_basis_integration_test.go` | — |
| DATA-12 | Типизированные документы | выполнено | `backend/internal/compliance/document_metadata.go`<br>`backend/internal/handlers/document_metadata.go`<br>`migrations/0808_document_metadata_and_legal_disputes.sql` | `backend/internal/compliance/document_metadata_test.go`<br>`backend/internal/handlers/document_metadata_integration_test.go` | `docs/TZ_4_4_FOUNDATION_PACKAGES.md` |
| DATA-13 | Legacy backfill справочников | выполнено | `backend/internal/backfill/directories.go`<br>`backend/internal/handlers/legacy_findings.go`<br>`migrations/0113_directory_backfill_findings.sql` | `backend/internal/backfill/directories_integration_test.go` | — |
| SEC-01 | Модель разрешений | выполнено | — | `backend/internal/rbac/rbac_test.go` | — |
| SEC-02 | Tenant и `SYSTEM_OWNER_ROLE` | выполнено | `backend/internal/dbx/permissions.go`<br>`backend/internal/handlers/admin.go`<br>`migrations/0105_system_owner_role.sql` | `backend/internal/handlers/instance_mode_integration_test.go`<br>`backend/internal/traceability/traceability_test.go` | — |
| SEC-03 | SUPER_ADMIN / HOLDING_ADMIN | выполнено | `backend/internal/bootstrap/admin.go` | `backend/internal/bootstrap/admin_integration_test.go` | — |
| SEC-04 | ORG_ADMIN / CURATOR и fallback | выполнено | `backend/internal/curators/curators.go`<br>`backend/internal/tasks/resolve.go`<br>`migrations/0809_workflow_tasks.sql` | `backend/internal/handlers/curator_scope_by_date_integration_test.go`<br>`backend/internal/handlers/workflow_tasks_integration_test.go` | — |
| SEC-05 | HR / FINANCE / LEGAL | выполнено | — | `backend/internal/handlers/field_authorization_test.go` | — |
| SEC-06 | AUDITOR_VIEWER | выполнено | — | `backend/internal/handlers/document_roles_test.go` | — |
| SEC-07 | MFA policy v4.4 | выполнено | `backend/internal/handlers/admin.go`<br>`backend/internal/handlers/auth_handlers.go`<br>`backend/internal/handlers/mfa_policy.go` | `backend/internal/handlers/mfa_policy_integration_test.go`<br>`frontend/architecture.test.cjs` | — |
| SEC-08 | Политика сессий | выполнено | `backend/internal/auth/session.go`<br>`migrations/0106_session_idle_timeout.sql` | `backend/internal/handlers/audit_and_session_integration_test.go` | — |
| SEC-09 | Security contract tests | частично | — | `backend/internal/handlers/rbac_matrix_test.go`<br>`backend/internal/handlers/role_category_matrix_integration_test.go`<br>`backend/internal/server/role_matrix_integration_test.go`<br>`backend/internal/traceability/traceability_test.go` | — |
| SEC-10 | Диспетчер задач и эскалаций | частично | `backend/internal/handlers/report_tasks.go`<br>`backend/internal/handlers/workflow_tasks.go`<br>`backend/internal/rbac/rbac.go`<br>`backend/internal/tasks/resolve.go` и ещё 2 | `backend/internal/handlers/report_tasks_integration_test.go`<br>`backend/internal/handlers/workflow_tasks_integration_test.go`<br>`frontend/architecture.test.cjs` | `docs/RUNBOOK.md` |
| TCH-01 | Модель педагогической нагрузки | выполнено | `backend/internal/handlers/entries.go`<br>`migrations/0301_teaching_load_atomic_key.sql` | `backend/internal/handlers/teaching_load_key_integration_test.go` | — |
| TCH-02 | Матрица семестров | выполнено | — | — | `docs/TEACHING_SEMESTERS.md`<br>`docs/TEACHING_SEMESTERS.md` |
| TCH-03 | Проверка стажа и ОКЗ | выполнено | `migrations/0300_teaching_staff_and_payouts.sql` | — | `docs/adr/ADR-16-it-experience.md` |
| TCH-04 | Расчёт стоимости | выполнено | — | `backend/internal/calculators/conformance_test.go` | — |
| TCH-05 | Документы преподавания | выполнено | — | `backend/internal/calculators/top_it_test.go` | `docs/adr/ADR-24-no-gph-surcharge.md` |
| TCH-06 | Компенсации финансиста | выполнено | `migrations/0300_teaching_staff_and_payouts.sql` | — | — |
| TCH-07 | Импорт/экспорт нагрузки | выполнено | — | `backend/internal/handlers/teaching_import_integration_test.go` | — |
| TCH-08 | Legacy backfill преподавателей | выполнено | `backend/internal/backfill/teachers.go`<br>`backend/internal/calculators/teachers.go`<br>`migrations/0302_legacy_backfill_findings.sql` | `backend/internal/backfill/teachers_integration_test.go` | — |
| OOP-01 | Модель ООП/РПД | выполнено | `migrations/0501_oop_rpd_typed_model.sql` | `backend/internal/handlers/oop_typed_model_integration_test.go` | — |
| OOP-02 | Нормативный расчёт | выполнено | — | `backend/internal/calculators/conformance_test.go` | — |
| OOP-03 | Документы и утверждение | выполнено | — | `backend/internal/compliance/documents_test.go` | — |
| OOP-04 | Обязательный минимум ВО | выполнено | `backend/internal/handlers/report_workflow.go` | `backend/internal/handlers/report_workflow_integration_test.go` | — |
| OOP-05 | Матрица агрегации | выполнено | — | `backend/internal/handlers/oop_matrix_integration_test.go` | — |
| OOP-06 | Legacy backfill | выполнено | `migrations/0501_oop_rpd_typed_model.sql` | `backend/internal/dbx/migrations_integration_test.go` | — |
| INT-01 | Карточка стажёра HR | не начато | — | — | `docs/adr/ADR-13-internship-agreement.md` |
| INT-02 | Входящие HR-документы | выполнено | — | `backend/internal/handlers/document_roles_test.go` | — |
| INT-03 | Наставник и приказ | выполнено | `migrations/0400_mentor_assignments.sql` | `backend/internal/handlers/tenant_and_mentor_integration_test.go` | — |
| INT-04 | Программа и табель куратора | не начато | — | — | — |
| INT-05 | Итоговая справка | выполнено | — | `backend/internal/handlers/document_roles_test.go` | — |
| INT-06 | Расчёт стажировки | выполнено | `backend/internal/handlers/annex4.go`<br>`backend/internal/handlers/internship_hours.go` | `backend/internal/handlers/internship_hours_test.go`<br>`backend/internal/handlers/regulatory_reports_test.go` | — |
| INT-07 | Compliance eligibility | не начато | — | — | — |
| INT-08 | Юридический аудит | выполнено | `backend/internal/handlers/document_metadata.go`<br>`migrations/0808_document_metadata_and_legal_disputes.sql` | `backend/internal/handlers/document_metadata_integration_test.go` | — |
| INT-09 | Отчёт по наставникам | выполнено | `backend/internal/handlers/regulatory_reports.go` | `backend/internal/handlers/internship_hours_test.go`<br>`backend/internal/handlers/regulatory_reports_test.go` | — |
| INT-10 | Legacy backfill | не начато | — | — | — |
| PRA-01 | Карточка практиканта | выполнено | `backend/internal/calculators/internship.go`<br>`migrations/0401_practice_records.sql` | `backend/internal/handlers/practice_typed_model_integration_test.go` | — |
| PRA-02 | Договор практической подготовки | выполнено | — | `backend/internal/handlers/document_roles_test.go` | — |
| PRA-03 | Проверка официального трудоустройства | выполнено | — | — | `docs/EMPLOYMENT_PRACTICE.md`<br>`docs/EMPLOYMENT_PRACTICE.md` |
| PRA-04 | Ограничения рабочего времени | частично | `backend/internal/calculators/internship.go`<br>`frontend/workspace.js` | `backend/internal/calculators/calculator_test.go`<br>`backend/internal/calculators/working_time_test.go`<br>`frontend/architecture.test.cjs`<br>`tests/workspace_integration_test.go` | — |
| PRA-05 | Расчёт и документы наставника | выполнено | — | `backend/internal/calculators/school_and_ministry_test.go` | — |
| PRA-06 | Legacy backfill | выполнено | `migrations/0401_practice_records.sql` | `backend/internal/handlers/practice_typed_model_integration_test.go` | — |
| TOP-01 | Паспорт топ-программы | выполнено | `migrations/0601_top_program_model.sql` | `backend/internal/handlers/top_program_integration_test.go` | — |
| TOP-02 | Денежное софинансирование | выполнено | `backend/internal/calculators/exact.go` | `backend/internal/calculators/top_it_test.go` | — |
| TOP-03 | Белый список расходов | не начато | — | — | `docs/adr/ADR-14-top-it-percent-denominator.md` |
| TOP-04 | Неденежная поддержка | выполнено | `backend/internal/handlers/top_items.go`<br>`backend/internal/topit/items.go`<br>`frontend/app.js`<br>`migrations/0601_top_program_model.sql` | `backend/internal/handlers/top_program_integration_test.go` | — |
| TOP-05 | Шкала прогресса | выполнено | — | `frontend/architecture.test.cjs` | — |
| TOP-06 | Стипендиаты | выполнено | `backend/internal/handlers/top_items.go`<br>`backend/internal/topit/items.go`<br>`frontend/app.js`<br>`migrations/0601_top_program_model.sql` | `backend/internal/handlers/top_program_integration_test.go` | — |
| TOP-07 | Производственные кейсы | выполнено | `backend/internal/handlers/top_items.go`<br>`backend/internal/topit/items.go`<br>`frontend/app.js`<br>`migrations/0601_top_program_model.sql` | `backend/internal/handlers/top_program_integration_test.go` | — |
| TOP-08 | Реестр РИД | выполнено | `backend/internal/topit/items.go`<br>`migrations/0602_top_rid_registry.sql` | `backend/internal/topit/items_test.go`<br>`frontend/architecture.test.cjs` | `docs/adr/ADR-14-top-it-percent-denominator.md` |
| TOP-09 | Документы АНО АЦ | выполнено | — | `backend/internal/compliance/documents_test.go` | — |
| TOP-10 | Правило п. 22 | выполнено | — | `backend/internal/handlers/calendar_deadline_test.go`<br>`backend/internal/handlers/report_workflow_integration_test.go` | `docs/adr/ADR-03-top-it-exemption.md` |
| TOP-11 | Legacy backfill | выполнено | `migrations/0601_top_program_model.sql`<br>`migrations/0811_top_legacy_eligibility.sql` | `backend/internal/dbx/migrations_integration_test.go` | — |
| MIN-01 | Карточка решения и поручения | выполнено | `backend/internal/compliance/readiness.go`<br>`migrations/0702_ministry_decision_card.sql` | — | — |
| MIN-02 | Динамические показатели | выполнено | `backend/internal/calculators/manual.go` | `backend/internal/calculators/school_and_ministry_test.go` | — |
| MIN-03 | Фактическая стоимость | выполнено | `backend/internal/calculators/manual.go`<br>`migrations/0701_ministry_cost_history.sql` | `backend/internal/calculators/school_and_ministry_test.go` | — |
| MIN-04 | Первичные документы | выполнено | `backend/internal/compliance/readiness.go` | `backend/internal/compliance/documents_test.go` | — |
| MIN-05 | Legacy backfill | выполнено | `backend/internal/compliance/readiness.go`<br>`migrations/0702_ministry_decision_card.sql` | — | — |
| SCH-01 | Общая модель школы/РОИВ | выполнено | `migrations/0703_school_typed_model.sql` | `backend/internal/handlers/school_typed_model_integration_test.go` | — |
| SCH-02 | Вид 6: программы и часы | выполнено | — | `backend/internal/calculators/school_and_ministry_test.go` | — |
| SCH-03 | Вид 7: повышение квалификации | выполнено | — | `backend/internal/calculators/school_and_ministry_test.go` | — |
| SCH-04 | Вид 8: цифровой контент | выполнено | — | `backend/internal/calculators/school_and_ministry_test.go` | — |
| SCH-05 | Логи ФГИС «Моя школа» | выполнено | `backend/internal/calculators/order_activities.go`<br>`backend/internal/compliance/readiness.go` | `backend/internal/calculators/calculator_test.go`<br>`backend/internal/calculators/digital_trace_test.go`<br>`backend/internal/compliance/risk_matrix_test.go` | — |
| SCH-06 | Запрет бюджетного финансирования | выполнено | `migrations/0703_school_typed_model.sql` | — | `docs/SCHOOL_FUNDING.md` |
| SCH-07 | Акты и риск-комплектность | выполнено | — | `backend/internal/compliance/school_readiness_test.go` | — |
| SCH-08 | Legacy backfill | выполнено | `migrations/0703_school_typed_model.sql` | `backend/internal/dbx/migrations_integration_test.go` | — |
| RISK-01 | Общий движок правил | выполнено | `backend/internal/modules/reporting/repository/activity_projection.go`<br>`backend/internal/risk/risk.go` | `backend/internal/dbx/no_manual_risk_test.go` | — |
| RISK-02 | Правила преподавателей | выполнено | — | `backend/internal/compliance/risk_matrix_test.go` | `docs/adr/ADR-16-it-experience.md` |
| RISK-03 | Правила стажировок/практики | выполнено | `frontend/app.js` | `backend/internal/compliance/risk_matrix_test.go`<br>`frontend/architecture.test.cjs` | — |
| RISK-04 | Правила ООП/РПД | выполнено | — | `backend/internal/compliance/risk_matrix_programs_test.go` | — |
| RISK-05 | Правила ТОП-ИТ | выполнено | — | `backend/internal/compliance/risk_matrix_programs_test.go` | — |
| RISK-06 | Правила школ | выполнено | — | `backend/internal/compliance/risk_matrix_programs_test.go` | — |
| RISK-07 | ComplianceContribution API | частично | `backend/internal/modules/reporting/repository/activity_projection.go`<br>`backend/internal/platform/activityprojection/projection.go` | — | — |
| RISK-08 | Пересчёт и кэш агрегатов | выполнено | `backend/internal/aggregates/aggregates.go`<br>`migrations/0810_activity_aggregates.sql` | `backend/internal/handlers/aggregates_integration_test.go` | — |
| DASH-01 | Агрегат 3% | выполнено | `backend/internal/handlers/dashboard.go` | `backend/internal/handlers/target_progress_test.go` | — |
| DASH-02 | Корзины риска | выполнено | `backend/internal/handlers/dashboard.go` | `backend/internal/handlers/dashboard_risk_test.go` | — |
| DASH-03 | Обязательные условия ВО | выполнено | `backend/internal/handlers/dashboard.go` | `backend/internal/handlers/mandatory_chips_test.go` | `docs/adr/ADR-03-top-it-exemption.md` |
| DASH-04 | Разбивка по видам | выполнено | `backend/internal/handlers/dashboard.go` | `backend/internal/handlers/dashboard_breakdown_test.go` | — |
| DASH-05 | Фильтры и delta | выполнено | `backend/internal/handlers/dashboard.go` | `backend/internal/handlers/dashboard_breakdown_test.go` | — |
| DASH-06 | API и performance | выполнено | `migrations/0108_entries_hot_path_indexes.sql` | `backend/internal/handlers/dashboard_load_integration_test.go`<br>`backend/internal/handlers/tenant_and_mentor_integration_test.go` | `docs/PERFORMANCE.md` |
| WF-01 | Статусы регламентного процесса | выполнено | `backend/internal/handlers/regulatory_processes.go`<br>`backend/internal/regulatory/calendar.go`<br>`backend/internal/regulatory/machine.go`<br>`migrations/0807_regulatory_processes.sql` | `backend/internal/handlers/regulatory_processes_integration_test.go` | `docs/TZ_4_4_FOUNDATION_PACKAGES.md` |
| WF-02 | Календарные deadline | выполнено | — | `backend/internal/handlers/calendar_deadline_test.go` | `docs/adr/ADR-02-calendar-policy.md` |
| WF-03 | Проект соглашения | выполнено | `backend/internal/handlers/regulatory_processes.go`<br>`backend/internal/regulatory/calendar.go`<br>`migrations/0807_regulatory_processes.sql` | `backend/internal/regulatory/machine_test.go` | — |
| WF-04 | Предварительный перечень | выполнено | `backend/internal/regulatory/calendar.go`<br>`migrations/0807_regulatory_processes.sql` | `backend/internal/handlers/regulatory_processes_integration_test.go`<br>`backend/internal/regulatory/machine_test.go`<br>`backend/internal/regulatory/store_integration_test.go` | — |
| WF-05 | Итоговый перечень и контрольные даты | выполнено | `backend/internal/handlers/regulatory_processes.go`<br>`backend/internal/handlers/report_calendar.go`<br>`backend/internal/regulatory/calendar.go`<br>`migrations/0807_regulatory_processes.sql` | `backend/internal/handlers/report_calendar_test.go`<br>`backend/internal/regulatory/machine_test.go`<br>`backend/internal/regulatory/store_integration_test.go` | — |
| WF-06 | Snapshot на 1 мая | частично | — | — | `docs/adr/ADR-20-snapshot-cutoff.md` |
| WF-07 | Snapshot API | выполнено | — | `backend/internal/handlers/limits_and_snapshots_test.go` | — |
| WF-08 | Источник «Факт на 1 мая» | выполнено | `backend/internal/handlers/annex4.go` | `backend/internal/handlers/tenant_and_mentor_integration_test.go` | — |
| REPORT-01 | Reporting projection | выполнено | `migrations/0805_generated_reports_csv.sql` | `backend/internal/handlers/tenant_and_mentor_integration_test.go` | — |
| REPORT-02 | Общий XLSX/DOCX toolkit | частично | `backend/internal/docx`<br>`backend/internal/xlsx` | `backend/internal/exchange/perf_test.go` | — |
| REPORT-03 | Приложение №4 XLSX | частично | — | `backend/internal/handlers/limits_and_snapshots_test.go` | `docs/ORDER_270_ADR_ANALYSIS.md` |
| REPORT-04 | Приложение №5, таблица 1 | выполнено | — | `backend/internal/handlers/regulatory_reports_test.go` | `docs/adr/ADR-20-snapshot-cutoff.md` |
| REPORT-05 | Приложение №1 к отчёту | выполнено | — | `backend/internal/handlers/regulatory_reports_test.go` | — |
| REPORT-06 | Приложение №2 к отчёту | выполнено | — | `backend/internal/handlers/regulatory_reports_test.go` | `docs/adr/ADR-13-internship-agreement.md` |
| REPORT-07 | Приложение №3 DOCX | выполнено | — | `backend/internal/handlers/regulatory_reports_test.go` | — |
| REPORT-08 | Типовые соглашения DOCX | выполнено | `migrations/0202_agreement_signatories.sql` | `backend/internal/handlers/agreement_template_test.go` | — |
| REPORT-09 | АНО АЦ: 6 листов | частично | `backend/internal/handlers/ano_ac_report.go`<br>`frontend/app.js`<br>`migrations/0812_ano_ac_report_type.sql` | `backend/internal/handlers/ano_ac_report_integration_test.go`<br>`frontend/architecture.test.cjs` | `docs/adr/ADR-14-top-it-percent-denominator.md` |
| REPORT-10 | Конструктор срезов | выполнено | `backend/internal/handlers/regulatory_reports.go`<br>`migrations/0805_generated_reports_csv.sql` | `backend/internal/handlers/regulatory_reports_test.go` | — |
| REPORT-11 | Реестр сформированных файлов | частично | `backend/internal/handlers/annex4.go`<br>`backend/internal/handlers/regulatory_reports.go` | `backend/internal/handlers/regulatory_reports_test.go` | — |
| REPORT-12 | Golden regression suite | частично | `backend/internal/xlsx` | — | `docs/ORDER_270_ADR_ANALYSIS.md` |
| CRYPTO-00 | Production security profile | не начато | `backend/internal/cryptoengine/fixture.go`<br>`backend/internal/cryptoengine/software.go` | — | `docs/PACKAGE_EXCHANGE.md` |
| CRYPTO-01 | Интерфейс CryptoEngine | выполнено | `backend/internal/cryptoengine` | — | — |
| CRYPTO-02 | Модель ключей и сертификатов | выполнено | `backend/internal/cryptoengine/certificates.go` | — | — |
| CRYPTO-03 | Versioned envelope `.pkg` | выполнено | `backend/internal/exchange/format.go` | `backend/internal/exchange/exchange_test.go` | — |
| CRYPTO-04 | Экспорт: sign + encrypt | частично | — | `backend/internal/exchange/exchange_test.go` | — |
| CRYPTO-05 | Импорт: decrypt + verify | частично | — | `backend/internal/exchange/exchange_test.go` | — |
| CRYPTO-06 | Composite matching | выполнено | — | `backend/internal/exchange/matching_test.go` | — |
| CRYPTO-07 | Diff Engine | выполнено | — | `backend/internal/exchange/diff_test.go` | — |
| CRYPTO-08 | Разрешение конфликтов | не начато | — | — | — |
| CRYPTO-09 | Протокол разногласий XLSX | выполнено | — | `backend/internal/exchange/protocol_test.go` | — |
| CRYPTO-10 | Fixture provider и CryptoPro adapter stub | выполнено | `backend/internal/cryptoengine` | — | — |
| CRYPTO-11 | Certificate/signature policy | выполнено | `backend/internal/cryptoengine/certificates.go` | `backend/internal/cryptoengine/certificates_test.go` | — |
| STORE-01 | Blob CAS | выполнено | `backend/internal/filestore/cas.go`<br>`backend/internal/handlers/attachments.go`<br>`backend/internal/retention/retention.go` | `backend/internal/filestore/cas_test.go` | `docs/adr/ADR-07-retention-policy.md` |
| STORE-02 | Метаданные документов | выполнено | — | `backend/internal/handlers/blob_sharing_integration_test.go`<br>`backend/internal/handlers/document_tenant_integration_test.go` | — |
| STORE-03 | zstd compression | выполнено | `backend/internal/filestore/cas.go` | `backend/internal/filestore/cas_test.go` | — |
| STORE-04 | Лимит 20 МБ | выполнено | — | `backend/internal/handlers/limits_and_snapshots_test.go` | — |
| STORE-05 | Antivirus pipeline | выполнено | `backend/internal/config/config.go`<br>`backend/internal/filestore/inspect.go`<br>`backend/internal/handlers/attachments.go`<br>`backend/internal/modules/documents/module.go` и ещё 1 | `backend/internal/handlers/antivirus_pipeline_integration_test.go` | — |
| STORE-06 | Миграция старых uploads | выполнено | `backend/cmd/storagemigrate/main.go`<br>`backend/internal/storagemigrate/migrate.go` | `backend/internal/storagemigrate/migrate_integration_test.go` | — |
| AUDIT-01 | Append-only Audit Trail | выполнено | `backend/internal/dbx/permissions.go`<br>`migrations/0901_append_only_audit_log.sql` | `backend/internal/dbx/permissions_integration_test.go`<br>`backend/internal/handlers/audit_and_session_integration_test.go` | — |
| AUDIT-02 | Защита целостности | выполнено | `migrations/0902_audit_hash_chain.sql` | `backend/internal/handlers/audit_chain_integration_test.go` | — |
| AUDIT-03 | Архивирование | выполнено | `migrations/0902_audit_hash_chain.sql` | `backend/internal/handlers/audit_chain_integration_test.go` | `docs/adr/ADR-07-retention-policy.md` |
| AUDIT-04 | Экспорт JSON | выполнено | `backend/internal/handlers/audit_export.go` | `backend/internal/handlers/audit_chain_integration_test.go` | — |
| UIR-01 | ВЫПОЛНЕНО | выполнено | `frontend/components/ui.js`<br>`frontend/theme.css` | `frontend/architecture.test.cjs` | — |
| UIR-02 | ВЫПОЛНЕНО | выполнено | `frontend/shell/app-shell.js`<br>`frontend/theme.css` | `frontend/architecture.test.cjs` | — |
| UIR-03 | ВЫПОЛНЕНО | выполнено | `frontend/components/ui.js` | — | — |
| UIR-04 | ВЫПОЛНЕНО | выполнено | `frontend/components/ui.js` | `frontend/architecture.test.cjs` | — |
| UIR-05 | ВЫПОЛНЕНО | выполнено | `frontend/components/ui.js` | — | — |
| UIR-06 | ВЫПОЛНЕНО | выполнено | `frontend/components/ui.js` | — | `docs/adr/ADR-21-product-brand.md` |
| UIR-07 | НЕ НАЧАТО | не начато | — | — | — |
| UI-00 | ВЫПОЛНЕНО | выполнено | `frontend/screens.js`<br>`frontend/shell/app-shell.js` | — | `docs/adr/ADR-21-product-brand.md` |
| UI-01 | ЧАСТИЧНО | частично | `frontend/app.js`<br>`frontend/screens/dashboard/index.js` | `frontend/architecture.test.cjs` | — |
| UI-02 | ЧАСТИЧНО | частично | `frontend/screens/partners/index.js` | — | — |
| UI-12 | ЧАСТИЧНО | частично | `frontend/components/ui.js` | `frontend/architecture.test.cjs`<br>`frontend/architecture.test.cjs` | — |
| UI-13 | ЧАСТИЧНО | частично | `frontend/theme.css` | `frontend/architecture.test.cjs` | — |
| UI-03 | Преподаватели | выполнено | `frontend/screens/teachers/index.js` | — | — |
| UI-04 | ООП/РПД | выполнено | `frontend/screens/ood-rpd/index.js` | — | — |
| UI-05 | Стажировки | частично | `frontend/app.js`<br>`frontend/workspace.js` | `frontend/architecture.test.cjs` | — |
| UI-06 | Практика | выполнено | `frontend/workspace.js` | `frontend/architecture.test.cjs` | — |
| UI-07 | ТОП-ИТ/ИИ | выполнено | `frontend/workspace.js` | `frontend/architecture.test.cjs` | `docs/adr/ADR-14-top-it-percent-denominator.md` |
| UI-08 | Школы | выполнено | `frontend/workspace.js` | `frontend/architecture.test.cjs` | — |
| UI-09 | Решение МЦ | выполнено | `frontend/screens/minc-decision/index.js` | — | — |
| UI-10 | Отчётность | не начато | — | — | — |
| UI-11 | Настройки | частично | — | `frontend/architecture.test.cjs` | — |
| OPS-01 | Встраивание SPA | выполнено | `backend/internal/modules/webapp/module.go` | `backend/internal/modules/webapp/module_test.go` | `docs/adr/ADR-05-frontend-delivery.md` |
| OPS-02 | TLS bootstrap | частично | `backend/cmd/tlsbootstrap/main.go`<br>`backend/internal/tlsboot/tlsboot.go`<br>`docker-compose.tls.yml` | `backend/internal/tlsboot/tlsboot_test.go` | `docs/RUNBOOK.md` |
| OPS-03 | nginx hardening/WAF | выполнено | — | `backend/internal/handlers/nginx_hardening_test.go` | — |
| OPS-04 | Offline container bundle | частично | `scripts/build-offline-bundle.sh`<br>`scripts/offline-install.sh` | — | `docs/RUNBOOK.md`<br>`docs/adr/ADR-05-frontend-delivery.md` |
| OPS-05 | Debian 12 hardened base | не начато | — | — | — |
| OPS-06 | First boot/recovery console | не начато | — | — | — |
| OPS-07 | OVA/OVF image | не начато | — | — | — |
| OPS-08 | QCOW2 image | не начато | — | — | — |
| OPS-09 | VHDX image | не начато | — | — | — |
| OPS-10 | Backup/restore v4.4 | выполнено | `backend/cmd/backupbundle/main.go`<br>`backend/internal/backup/bundle.go` | `backend/internal/backup/backup_test.go`<br>`backend/internal/backup/drill_integration_test.go` | `docs/RUNBOOK.md`<br>`docs/adr/ADR-07-retention-policy.md` |
| OPS-11 | Upgrade/rollback | частично | `backend/cmd/upgradecheck/main.go`<br>`backend/internal/upgrade/upgrade.go`<br>`scripts/deploy-safe.sh` | — | `docs/RUNBOOK.md`<br>`docs/adr/ADR-05-frontend-delivery.md` |
| OPS-12 | Monitoring/runbook | выполнено | `backend/internal/handlers/operations_metrics.go` | `backend/internal/handlers/operations_metrics_integration_test.go` | `docs/RUNBOOK.md` |
| QA-01 | Formula conformance | выполнено | — | `backend/internal/calculators/conformance_test.go` | — |
| QA-02 | RBAC/tenant/fallback conformance | выполнено | — | `backend/internal/handlers/tenant_conformance_integration_test.go`<br>`backend/internal/rbac/rbac_test.go`<br>`backend/internal/server/endpoint_matrix_test.go` | — |
| QA-03 | Risk matrix conformance | выполнено | — | `backend/internal/compliance/manual_risk_test.go` | — |
| QA-04 | Workflow/timer simulation | выполнено | — | `backend/internal/handlers/timer_simulation_test.go` | `docs/adr/ADR-20-snapshot-cutoff.md` |
| QA-05 | Report golden files | не начато | — | — | — |
| QA-06 | Crypto security tests | частично | — | `backend/internal/exchange/exchange_test.go` | — |
| QA-07 | Storage security tests | выполнено | — | `backend/internal/filestore/concurrency_test.go`<br>`backend/internal/handlers/upload_quota_race_integration_test.go` | — |
| QA-08 | API contract suite | выполнено | — | `backend/internal/apicontract/coverage_test.go`<br>`backend/internal/apicontract/dto_schema_test.go` | — |
| QA-09 | E2E по 11 экранам | не начато | — | — | — |
| QA-10 | Performance/load | частично | `migrations/0108_entries_hot_path_indexes.sql` | `backend/internal/backup/backup_test.go`<br>`backend/internal/exchange/perf_test.go`<br>`backend/internal/handlers/dashboard_load_integration_test.go`<br>`backend/internal/handlers/reports_load_integration_test.go` и ещё 1 | `docs/PERFORMANCE.md` |
| QA-11 | Disaster recovery drill | частично | `backend/cmd/backupbundle/main.go`<br>`backend/internal/backup/drill.go` | `backend/internal/backup/drill_integration_test.go` | `docs/RUNBOOK.md` |
| QA-12 | Offline acceptance | не начато | — | — | — |
| INTG-01 | Подключение backend-модулей | выполнено | — | `backend/internal/server/composition_test.go` | — |
| INTG-02 | Подключение frontend-экранов | выполнено | `frontend/screens.js` | `frontend/screens.test.cjs` | — |
| INTG-03 | Сводный compliance projection | частично | `backend/internal/platform/activityprojection/projection.go` | `backend/internal/platform/activityprojection/projection_test.go` | — |
| INTG-04 | Legacy cutover | частично | `backend/internal/backfill/reconcile.go` | `backend/internal/backfill/directories_integration_test.go` | — |
| INTG-05 | Полный регрессионный прогон | выполнено | `scripts/verify.sh` | — | — |
| INTG-06 | Приёмочная трассировка ТЗ | частично | `backend/internal/traceability/traceability.go` | — | — |
