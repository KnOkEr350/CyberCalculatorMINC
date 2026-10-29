ALTER TABLE agreements DROP CONSTRAINT agreements_signature_method_check;
ALTER TABLE agreements ADD CONSTRAINT agreements_signature_method_check
  CHECK (signature_method IN ('unsigned','paper','qualified_electronic','goskey','edo','gosedo'));
