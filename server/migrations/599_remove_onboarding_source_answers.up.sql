UPDATE "user"
SET onboarding_questionnaire = onboarding_questionnaire
    - 'source' - 'source_other' - 'source_skipped'
WHERE jsonb_typeof(onboarding_questionnaire) = 'object'
  AND onboarding_questionnaire ?| ARRAY['source', 'source_other', 'source_skipped'];
