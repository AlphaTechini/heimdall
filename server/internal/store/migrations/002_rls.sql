-- Hosted Postgres such as Supabase exposes the public schema through its Data API (anon key).
-- Row-level security with no policies closes that path; heimdalld connects as the table owner,
-- which bypasses RLS, so nothing changes for the server or for plain Postgres.
DO $$
DECLARE t text;
BEGIN
  FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = current_schema() LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  END LOOP;
END $$;
