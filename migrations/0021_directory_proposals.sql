-- Moderator suggestions are visible in the directory but cannot become
-- selectable partners until an administrator reviews them.
ALTER TABLE education_directory
  ADD COLUMN proposed_by UUID REFERENCES users(id),
  ADD COLUMN proposal_comment TEXT,
  ADD COLUMN proposed_at TIMESTAMPTZ,
  ADD COLUMN reviewed_by UUID REFERENCES users(id),
  ADD COLUMN reviewed_at TIMESTAMPTZ,
  ADD COLUMN review_comment TEXT;

CREATE INDEX education_directory_pending_proposals_idx
  ON education_directory(proposed_at,id)
  WHERE proposed_by IS NOT NULL AND verification_status='pending';

CREATE INDEX education_directory_proposed_by_idx
  ON education_directory(proposed_by,proposed_at DESC)
  WHERE proposed_by IS NOT NULL;
