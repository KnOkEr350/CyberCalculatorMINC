-- An IT organization may participate in only one legal-entity group.
CREATE UNIQUE INDEX legal_entity_group_member_single_it_group
  ON legal_entity_group_members(inn) WHERE is_it_organization;

-- Eligibility is the final accounting boundary used by dashboards and
-- exports. Keep the legal checks in the view as well as in the HTTP workflow:
-- an approved row based on actual costs is never eligible without the
-- auditor confirmation, and the TOP IT exception never crosses a tenant.
CREATE OR REPLACE VIEW entry_eligibility AS
SELECT e.id,e.partner_id,e.agreement_id,e.report_year,e.period_type,e.amount_rub,
  COALESCE(r.status,'draft') AS report_status,
  COALESCE(
    r.status='approved'
    AND r.scope_confirmed AND r.conditions_confirmed AND r.evidence_confirmed
    AND (e.period_type='plan' OR r.counterparty_confirmed)
    AND (e.cost_method<>'actual' OR (
      r.actual_costs_confirmed AND NULLIF(trim(r.auditor_report_reference),'') IS NOT NULL))
    AND a.status='active'
    AND a.valid_from<=make_date(e.report_year,12,31)
    AND a.valid_until>=make_date(e.report_year,1,1)
    AND (a.agreement_kind<>'roiv' OR EXISTS(
      SELECT 1 FROM regional_authorities ra
      WHERE ra.id=a.regional_authority_id AND ra.status='active'))
    AND EXISTS(SELECT 1 FROM agreement_responsible_people rp
      WHERE rp.agreement_id=a.id AND rp.party='cyberprotect')
    AND EXISTS(SELECT 1 FROM agreement_responsible_people rp
      WHERE rp.agreement_id=a.id AND rp.party='counterparty')
    AND (
      NOT EXISTS(
        SELECT 1 FROM agreement_activity_requirements req
        WHERE req.agreement_id=e.agreement_id AND NOT EXISTS(
          SELECT 1 FROM entries covered
          WHERE covered.agreement_id=e.agreement_id
            AND covered.report_year=e.report_year
            AND covered.period_type=e.period_type
            AND covered.category_code=req.category_code
            AND covered.amount_rub>0))
      OR (
        a.agreement_kind='education_organization'
        AND EXISTS(SELECT 1 FROM entries top_entry
          WHERE top_entry.agreement_id=e.agreement_id
            AND top_entry.report_year=e.report_year
            AND top_entry.period_type=e.period_type
            AND top_entry.category_code='top_it' AND top_entry.amount_rub>0)
        AND EXISTS(
          SELECT 1 FROM agreements other
          JOIN agreement_reports other_report ON other_report.agreement_id=other.id
            AND other_report.report_year=e.report_year
            AND other_report.period_type=e.period_type
            AND other_report.status='approved'
          JOIN agreement_partners other_partner ON other_partner.agreement_id=other.id
          JOIN partners other_organization ON other_organization.id=other_partner.partner_id
            AND other_organization.partner_kind<>'school'
          WHERE other.id<>e.agreement_id AND other.status='active'
            AND other.it_company_id=a.it_company_id
            AND EXISTS(SELECT 1 FROM agreement_partners current_partner
              WHERE current_partner.agreement_id=e.agreement_id
                AND current_partner.partner_id<>other_partner.partner_id)
            AND EXISTS(SELECT 1 FROM entries x WHERE x.agreement_id=other.id AND x.report_year=e.report_year AND x.period_type=e.period_type AND x.category_code='teachers' AND x.amount_rub>0)
            AND EXISTS(SELECT 1 FROM entries x WHERE x.agreement_id=other.id AND x.report_year=e.report_year AND x.period_type=e.period_type AND x.category_code='ood_rpd' AND x.amount_rub>0)
            AND EXISTS(SELECT 1 FROM entries x WHERE x.agreement_id=other.id AND x.report_year=e.report_year AND x.period_type=e.period_type AND x.category_code IN ('internship','employment_practice') AND x.amount_rub>0)
            AND EXISTS(SELECT 1 FROM entries x WHERE x.agreement_id=other.id AND x.report_year=e.report_year AND x.period_type=e.period_type AND x.category_code='minc_decision' AND x.amount_rub>0)
        )
      )
    ),FALSE) AS eligible
FROM entries e
JOIN agreements a ON a.id=e.agreement_id
LEFT JOIN agreement_reports r ON r.agreement_id=e.agreement_id
  AND r.report_year=e.report_year AND r.period_type=e.period_type;
