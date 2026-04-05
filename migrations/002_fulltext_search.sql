ALTER TABLE meetings ADD COLUMN IF NOT EXISTS tsv tsvector;

UPDATE meetings SET tsv = to_tsvector('russian', coalesce(transcription, ''))
WHERE tsv IS NULL;

CREATE INDEX IF NOT EXISTS idx_meetings_tsv ON meetings USING GIN(tsv);

CREATE OR REPLACE FUNCTION meetings_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.tsv := to_tsvector('russian', coalesce(NEW.transcription, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS meetings_tsv_update ON meetings;
CREATE TRIGGER meetings_tsv_update
    BEFORE INSERT OR UPDATE ON meetings
    FOR EACH ROW EXECUTE FUNCTION meetings_tsv_trigger();
