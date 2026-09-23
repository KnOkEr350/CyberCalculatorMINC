-- REPORT-08: both parties and their authority basis are stored, not printed
-- as anonymous placeholders in the standard agreement.
ALTER TABLE agreements
  ADD COLUMN company_signer_authority TEXT,
  ADD COLUMN counterparty_signer_name TEXT,
  ADD COLUMN counterparty_signer_position TEXT,
  ADD COLUMN counterparty_signer_authority TEXT;

ALTER TABLE agreements ADD CONSTRAINT agreements_signatory_lengths CHECK (
  length(COALESCE(company_signer_authority,'')) <= 1000
  AND length(COALESCE(counterparty_signer_name,'')) <= 300
  AND length(COALESCE(counterparty_signer_position,'')) <= 300
  AND length(COALESCE(counterparty_signer_authority,'')) <= 1000
);

ALTER TABLE agreements ADD CONSTRAINT agreements_active_signatories_complete CHECK (
  status <> 'active'
  OR (
    length(btrim(COALESCE(company_signer_authority,''))) > 0
    AND length(btrim(COALESCE(counterparty_signer_name,''))) > 0
    AND length(btrim(COALESCE(counterparty_signer_authority,''))) > 0
  )
) NOT VALID;
