# Общая UI-библиотека

BASE-06 реализован в `frontend/components/ui.js`. Библиотека загружается до
legacy-слоя и публикует стабильный объект `CyberCalcUI`, поэтому существующие
экраны могут переходить на компоненты постепенно.

Доступны генераторы таблицы, фильтров, правого drawer, upload/dropzone,
risk badge, денежных и календарных полей, а также toast. Поведенческие helpers
покрывают focus trap, клавиатурный upload и reorder через кнопки, `Alt+↑/↓` и
drag-and-drop. Любая предметная операция всё равно обязана отдельно проверить
permission и optimistic lock на backend.

Денежное поле парсится `parseMoney()` в целое количество копеек. Даты остаются
строками `YYYY-MM-DD`. HTML из данных экранируется по умолчанию; callback
`column.render` таблицы предназначен только для заранее безопасной разметки.

Component tests находятся в `frontend/components/ui.test.cjs` и запускаются
общей командой `node frontend/check.mjs`.
