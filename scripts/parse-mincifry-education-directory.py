#!/usr/bin/env python3
"""Parse the text table attached to Minцифры Order No. 27.

The PDF exports each organization as: name, a run of program codes, then the
same number of program names.  Lines may be wrapped and single-row records may
be flattened, so the parser validates every code against its canonical title
instead of relying on line boundaries.
"""

from __future__ import annotations

import argparse
import csv
import json
import re
import sys
from dataclasses import dataclass


PROGRAMS = {
    "01.03.01": "Математика",
    "01.03.02": "Прикладная математика и информатика",
    "01.03.03": "Механика и математическое моделирование",
    "01.03.04": "Прикладная математика",
    "01.04.02": "Прикладная математика и информатика",
    "01.04.03": "Механика и математическое моделирование",
    "01.04.04": "Прикладная математика",
    "01.05.01": "Фундаментальные математика и механика",
    "02.03.01": "Математика и компьютерные науки",
    "02.03.02": "Фундаментальная информатика и информационные технологии",
    "02.03.03": "Математическое обеспечение и администрирование информационных систем",
    "02.04.01": "Математика и компьютерные науки",
    "02.04.02": "Фундаментальная информатика и информационные технологии",
    "02.04.03": "Математическое обеспечение и администрирование информационных систем",
    "03.03.01": "Прикладные математика и физика",
    "03.04.01": "Прикладные математика и физика",
    "03.05.02": "Фундаментальная и прикладная физика",
    "05.03.03": "Картография и геоинформатика",
    "06.05.01": "Биоинженерия и биоинформатика",
    "09.03.01": "Информатика и вычислительная техника",
    "09.03.02": "Информационные системы и технологии",
    "09.03.03": "Прикладная информатика",
    "09.03.04": "Программная инженерия",
    "09.04.01": "Информатика и вычислительная техника",
    "09.04.02": "Информационные системы и технологии",
    "09.04.03": "Прикладная информатика",
    "09.04.04": "Программная инженерия",
    "10.03.01": "Информационная безопасность",
    "10.04.01": "Информационная безопасность",
    "10.05.01": "Компьютерная безопасность",
    "10.05.02": "Информационная безопасность телекоммуникационных систем",
    "10.05.03": "Информационная безопасность автоматизированных систем",
    "10.05.04": "Информационно-аналитические системы безопасности",
    "10.05.05": "Безопасность информационных технологий в правоохранительной сфере",
    "11.03.01": "Радиотехника",
    "11.03.02": "Инфокоммуникационные технологии и системы связи",
    "11.03.03": "Конструирование и технология электронных средств",
    "11.03.04": "Электроника и наноэлектроника",
    "11.04.01": "Радиотехника",
    "11.04.02": "Инфокоммуникационные технологии и системы связи",
    "11.04.03": "Конструирование и технология электронных средств",
    "11.04.04": "Электроника и наноэлектроника",
    "11.05.01": "Радиоэлектронные системы и комплексы",
    "11.05.02": "Специальные радиотехнические системы",
    "12.03.03": "Фотоника и оптоинформатика",
    "12.05.01": "Электронные и оптико-электронные приборы и системы специального назначения",
    "13.03.01": "Теплоэнергетика и теплотехника",
    "13.03.02": "Электроэнергетика и электротехника",
    "13.03.03": "Энергетическое машиностроение",
    "14.03.01": "Ядерная энергетика и теплофизика",
    "14.03.02": "Ядерные физика и технологии",
    "14.05.01": "Ядерные реакторы и материалы",
    "14.05.02": "Атомные станции: проектирование, эксплуатация и инжиниринг",
    "15.03.01": "Машиностроение",
    "15.03.02": "Технологические машины и оборудование",
    "15.03.03": "Прикладная механика",
    "15.03.04": "Автоматизация технологических процессов и производств",
    "15.03.05": "Конструкторско-технологическое обеспечение машиностроительных производств",
    "15.03.06": "Мехатроника и робототехника",
    "15.04.01": "Машиностроение",
    "15.04.02": "Технологические машины и оборудование",
    "15.04.03": "Прикладная механика",
    "15.04.04": "Автоматизация технологических процессов и производств",
    "15.04.05": "Конструкторско-технологическое обеспечение машиностроительных производств",
    "15.04.06": "Мехатроника и робототехника",
    "15.05.01": "Проектирование технологических машин и комплексов",
    "17.05.03": "Проектирование, производство и испытание корабельного вооружения и информационно-управляющих систем",
    "24.03.01": "Ракетные комплексы и космонавтика",
    "24.03.02": "Системы управления движением и навигация",
    "24.03.03": "Баллистика и гидроаэродинамика",
    "24.03.04": "Авиастроение",
    "24.03.05": "Двигатели летательных аппаратов",
    "24.04.01": "Ракетные комплексы и космонавтика",
    "24.04.03": "Баллистика и гидроаэродинамика",
    "24.04.04": "Авиастроение",
    "24.04.05": "Двигатели летательных аппаратов",
    "24.05.01": "Проектирование, производство и эксплуатация ракет и ракетно-космических комплексов",
    "24.05.02": "Проектирование авиационных и ракетных двигателей",
    "24.05.03": "Испытание летательных аппаратов",
    "24.05.06": "Системы управления летательными аппаратами",
    "24.05.07": "Самолето- и вертолетостроение",
    "26.03.02": "Кораблестроение, океанотехника и системотехника объектов морской инфраструктуры",
    "26.05.01": "Проектирование и постройка кораблей, судов и объектов океанотехники",
    "26.05.02": "Проектирование, изготовление и ремонт энергетических установок и систем автоматизации кораблей и судов",
    "27.03.02": "Управление качеством",
    "27.03.03": "Системный анализ и управление",
    "27.03.04": "Управление в технических системах",
    "27.03.05": "Инноватика",
    "27.04.03": "Системный анализ и управление",
    "27.04.04": "Управление в технических системах",
    "27.04.05": "Инноватика",
    "30.05.03": "Медицинская кибернетика",
    "38.03.05": "Бизнес-информатика",
    "38.04.05": "Бизнес-информатика",
    "45.03.04": "Интеллектуальные системы в гуманитарной сфере",
    "45.04.04": "Интеллектуальные системы в гуманитарной среде",
}

CODE_RE = re.compile(r"(?<!\d)\d{2}\.\d{2}\.\d{2}(?!\d)")


@dataclass(frozen=True)
class Record:
    name: str
    codes: tuple[str, ...]


def compact(value: str) -> str:
    return re.sub(r"\s+", "", value.replace("\u00ad", "").replace("–", "-").replace("—", "-"))


def consume_title(text: str, start: int, expected: str) -> int:
    target = compact(expected)
    collected = ""
    index = start
    while index < len(text) and len(collected) < len(target):
        char = text[index]
        index += 1
        if char.isspace() or char == "\u00ad":
            continue
        if char in "–—":
            char = "-"
        collected += char
        if not target.startswith(collected):
            excerpt = re.sub(r"\s+", " ", text[start : start + 180]).strip()
            raise ValueError(f"expected {expected!r}, got {excerpt!r}")
    if collected != target:
        raise ValueError(f"truncated title: expected {expected!r}")
    return index


def parse(text: str) -> list[Record]:
    text = text.replace("\r\n", "\n").replace("\r", "\n").replace("\u00a0", " ")
    first = text.find("Нижегородский филиал")
    if first < 0:
        raise ValueError("table start was not found")
    position = first
    records: list[Record] = []
    while True:
        first_code = CODE_RE.search(text, position)
        if first_code is None:
            tail = re.sub(r"\s+", " ", text[position:]).strip()
            if tail:
                raise ValueError(f"unparsed table tail: {tail[:180]!r}")
            break
        name = re.sub(r"\s+", " ", text[position:first_code.start()]).strip()
        if not name:
            raise ValueError(f"empty organization name before offset {first_code.start()}")

        codes: list[str] = []
        cursor = first_code.start()
        while True:
            match = CODE_RE.match(text, cursor)
            if match is None:
                break
            code = match.group(0)
            if code not in PROGRAMS:
                raise ValueError(f"unknown program code {code}")
            codes.append(code)
            cursor = match.end()
            whitespace_end = cursor
            while whitespace_end < len(text) and text[whitespace_end].isspace():
                whitespace_end += 1
            if CODE_RE.match(text, whitespace_end) is None:
                cursor = whitespace_end
                break
            cursor = whitespace_end

        for code in codes:
            while cursor < len(text) and text[cursor].isspace():
                cursor += 1
            cursor = consume_title(text, cursor, PROGRAMS[code])
        records.append(Record(name=name, codes=tuple(dict.fromkeys(codes))))
        position = cursor
    return records


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--format", choices=("json", "tsv", "migration"), default="json")
    args = parser.parse_args()
    records = parse(sys.stdin.read())
    relation_count = sum(len(row.codes) for row in records)
    if args.format == "migration" and (len(records) != 642 or relation_count != 4833):
        raise ValueError(
            f"official table invariant failed: {len(records)} organizations, "
            f"{relation_count} organization-program relations"
        )
    if args.format == "json":
        json.dump([{"name": row.name, "codes": list(row.codes)} for row in records], sys.stdout, ensure_ascii=False, indent=2)
        sys.stdout.write("\n")
    elif args.format == "tsv":
        writer = csv.writer(sys.stdout, delimiter="\t", lineterminator="\n")
        writer.writerow(("name", "program_codes"))
        for row in records:
            writer.writerow((row.name, ",".join(row.codes)))
    else:
        source_url = "https://adm.digital.gov.ru/app/uploads/2026/05/6027f0_perechen-oo-vo-realizuyushhih-it-speczialnosti.pdf"
        print("-- 642 educational organizations listed for Order No. 27 of 22.01.2026.")
        print("-- Generated by scripts/parse-mincifry-education-directory.py from the official PDF text.")
        print("ALTER TABLE education_directory")
        print("  ADD COLUMN listed_in_mincifry_order_27 BOOLEAN NOT NULL DEFAULT FALSE;\n")
        print("CREATE TEMP TABLE mincifry_order_27_import (")
        print("  name TEXT PRIMARY KEY,")
        print("  program_codes TEXT[] NOT NULL")
        print(") ON COMMIT DROP;\n")
        print("INSERT INTO mincifry_order_27_import(name, program_codes) VALUES")
        values = []
        for row in records:
            name = row.name.replace("'", "''")
            codes = ",".join("'" + code + "'" for code in row.codes)
            values.append(f"('{name}',ARRAY[{codes}]::TEXT[])")
        print(",\n".join(values) + ";\n")
        legal_forms = (
            "федеральное государственное автономное образовательное учреждение высшего образования",
            "федерального государственного автономного образовательного учреждения высшего образования",
            "федеральное государственное бюджетное образовательное учреждение высшего образования",
            "федерального государственного бюджетного образовательного учреждения высшего образования",
            "фгаоу во",
            "фгбоу во",
            "фгобу во",
            "фгбу во",
            "фгбоу иво",
            "фгбу вон",
        )
        normalized_value = "replace(lower(replace(%s,'ё','е')),'им.','имени')"
        for legal_form in legal_forms:
            normalized_value = f"replace({normalized_value},'{legal_form}','')"
        # Explicit alphabets keep matching deterministic under PostgreSQL's C locale.
        normalized = f"regexp_replace({normalized_value},'[^0-9a-zа-я]','','g')"
        print("-- Reuse one monitoring row with the same legal-name core, preserving its region and links.")
        print("WITH exact_matches AS (")
        print("  SELECT DISTINCT ON (src.name) d.id,src.program_codes")
        print("  FROM mincifry_order_27_import src")
        print("  JOIN education_directory d ON d.partner_kind='vuz'")
        print(f"    AND {normalized % 'd.name'} = {normalized % 'src.name'}")
        print("  ORDER BY src.name,(d.verification_status='verified') DESC,d.id")
        print(")")
        print("UPDATE education_directory d SET")
        print("  program_codes = src.program_codes,")
        print(f"  programs_source_url = '{source_url}',")
        print("  programs_checked_at = now(),")
        print("  listed_in_mincifry_order_27 = TRUE,")
        print("  updated_at = now()")
        print("FROM exact_matches src")
        print("WHERE d.id=src.id;\n")
        print("-- The order contains short names but no region or licensing attributes.")
        print("-- Such rows enter the review queue; administrators enrich and verify them later.")
        print("INSERT INTO education_directory(")
        print("  name,partner_kind,region,source,registry_record_id,source_url,registry_updated_at,")
        print("  verification_status,program_codes,programs_source_url,programs_checked_at,")
        print("  listed_in_mincifry_order_27")
        print(")")
        print("SELECT src.name,'vuz','Регион не указан','Перечень ОО ВО Минцифры России, приказ № 27 от 22.01.2026',")
        print("  'mincifry-order-27-2026:' || md5(src.name),")
        print(f"  '{source_url}',DATE '2026-01-22','pending',src.program_codes,")
        print(f"  '{source_url}',now(),TRUE")
        print("FROM mincifry_order_27_import src")
        print("WHERE NOT EXISTS (")
        print("  SELECT 1 FROM education_directory d")
        print(f"  WHERE d.partner_kind='vuz' AND {normalized % 'd.name'} = {normalized % 'src.name'}")
        print(")")
        print("ON CONFLICT (name,partner_kind,region) DO UPDATE SET")
        print("  program_codes=EXCLUDED.program_codes,")
        print("  programs_source_url=EXCLUDED.programs_source_url,")
        print("  programs_checked_at=EXCLUDED.programs_checked_at,")
        print("  listed_in_mincifry_order_27=TRUE,")
        print("  updated_at=now();\n")
        print("CREATE INDEX education_directory_mincifry_order_27_idx")
        print("  ON education_directory(listed_in_mincifry_order_27, verification_status)")
        print("  WHERE listed_in_mincifry_order_27;\n")
        print("COMMENT ON COLUMN education_directory.listed_in_mincifry_order_27 IS")
        print("  'Organization is explicitly listed in the Minцифры Order No. 27 table supplied from the official PDF.';")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
