-- A calculation is not an approval. Keep raw amounts and expose separately
-- whether the mandatory-activity prerequisites have been entered.
CREATE VIEW entry_eligibility AS
WITH activity_status AS (
  SELECT partner_id,report_year,period_type,
    bool_or(category_code='teachers' AND amount_rub>0) AS teachers,
    bool_or(category_code='ood_rpd' AND amount_rub>0) AS programs,
    bool_or(category_code='top_it' AND amount_rub>0) AS top_it,
    bool_or(category_code<>'top_it' AND amount_rub>0) AS other_activity
  FROM entries GROUP BY partner_id,report_year,period_type
)
SELECT e.id,e.partner_id,e.report_year,e.period_type,e.amount_rub,
  CASE
    WHEN p.id IS NULL THEN false
    WHEN p.partner_kind<>'vuz' THEN true
    WHEN e.category_code IN ('teachers','ood_rpd') THEN true
    WHEN s.top_it THEN EXISTS(SELECT 1 FROM activity_status other
      WHERE other.partner_id<>e.partner_id AND other.report_year=e.report_year
        AND other.period_type=e.period_type AND other.other_activity)
    ELSE s.teachers AND s.programs
  END AS eligible
FROM entries e LEFT JOIN partners p ON p.id=e.partner_id
LEFT JOIN activity_status s ON s.partner_id=e.partner_id AND s.report_year=e.report_year AND s.period_type=e.period_type;
