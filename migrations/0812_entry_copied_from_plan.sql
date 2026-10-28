-- Факт можно создать из записи плана. Связь хранится в самой записи: так
-- повторное копирование не плодит дубли, а факт остаётся прослеживаемым до
-- плана. Удаление записи плана связь сбрасывает, факт при этом сохраняется.
ALTER TABLE entries
  ADD COLUMN copied_from_entry_id UUID REFERENCES entries(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX entries_copied_from_entry_uidx
  ON entries(copied_from_entry_id)
  WHERE copied_from_entry_id IS NOT NULL;
