-- Outside collaborators (LIGH-282). A workspace names its guests by email in
-- workspace.settings.lightsoft_guests (a JSON array of lowercase emails); the
-- member row keeps role "member", so the schema stays upstream's.

-- name: ListLightsoftGuestWorkspaces :many
-- Every workspace that names at least one guest, with the list.
SELECT id, (settings->'lightsoft_guests')::jsonb AS guests
FROM workspace
WHERE jsonb_typeof(settings->'lightsoft_guests') = 'array'
  AND jsonb_array_length(settings->'lightsoft_guests') > 0;

-- name: GetLightsoftGuestEmails :one
-- The stored guest list of one workspace ('[]' when it names none).
SELECT COALESCE(
  CASE WHEN jsonb_typeof(settings->'lightsoft_guests') = 'array' THEN settings->'lightsoft_guests' END,
  '[]'::jsonb)::jsonb AS guests
FROM workspace
WHERE id = @workspace_id;

-- name: SetLightsoftGuestEmails :exec
UPDATE workspace
SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{lightsoft_guests}', @guests::jsonb, true),
    updated_at = now()
WHERE id = @workspace_id;

-- name: ListLightsoftGuestHiddenProjectIDs :many
-- What a guest may not see beyond private projects: every project of the
-- workspace they are not a project member of, whatever its visibility, plus
-- the nil uuid, which projectaccess reads as "objects outside any project".
-- Empty for anyone who is not a guest of the workspace.
WITH guest AS (
  SELECT 1 AS one
  FROM workspace w
  JOIN "user" u ON u.id = @user_id::uuid
  WHERE w.id = @workspace_id::uuid
    AND EXISTS (
      SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(w.settings->'lightsoft_guests') = 'array'
        THEN w.settings->'lightsoft_guests' ELSE '[]'::jsonb END) AS g(email)
      WHERE lower(btrim(g.email)) = lower(u.email)
    )
)
SELECT p.id
FROM project p, guest
WHERE p.workspace_id = @workspace_id::uuid
  AND NOT EXISTS (
    SELECT 1 FROM project_member pm
    WHERE pm.project_id = p.id AND pm.user_id = @user_id::uuid
  )
UNION ALL
SELECT '00000000-0000-0000-0000-000000000000'::uuid FROM guest;

-- name: CountLightsoftGuestsAmong :one
-- How many of user_ids are guests of the workspace.
SELECT count(*)::int
FROM member m
JOIN "user" u ON u.id = m.user_id
JOIN workspace w ON w.id = m.workspace_id
WHERE m.workspace_id = @workspace_id::uuid
  AND m.user_id = ANY(@user_ids::uuid[])
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(w.settings->'lightsoft_guests') = 'array'
      THEN w.settings->'lightsoft_guests' ELSE '[]'::jsonb END) AS g(email)
    WHERE lower(btrim(g.email)) = lower(u.email)
  );

-- name: ListLightsoftGuestProjectMemberships :many
-- The current guest members of a workspace and the projects each one is a
-- project member of (project_id is NULL for a guest in no project).
SELECT m.user_id, p.id AS project_id, p.title AS project_title
FROM member m
JOIN "user" u ON u.id = m.user_id
JOIN workspace w ON w.id = m.workspace_id
LEFT JOIN project_member pm ON pm.user_id = m.user_id AND pm.workspace_id = m.workspace_id
LEFT JOIN project p ON p.id = pm.project_id AND p.workspace_id = m.workspace_id
WHERE m.workspace_id = @workspace_id::uuid
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(w.settings->'lightsoft_guests') = 'array'
      THEN w.settings->'lightsoft_guests' ELSE '[]'::jsonb END) AS g(email)
    WHERE lower(btrim(g.email)) = lower(u.email)
  );

-- name: ListLightsoftGuestMembers :many
-- Guest list entries of a workspace with their account and membership, for
-- the admin screen. An entry without an account yet has NULL user fields.
SELECT lower(btrim(g.email))::text AS email,
       u.id AS user_id,
       u.name AS user_name,
       m.id AS member_id,
       m.created_at AS joined_at
FROM workspace w
CROSS JOIN LATERAL jsonb_array_elements_text(
  CASE WHEN jsonb_typeof(w.settings->'lightsoft_guests') = 'array' THEN w.settings->'lightsoft_guests' ELSE '[]'::jsonb END
) AS g(email)
LEFT JOIN "user" u ON lower(u.email) = lower(btrim(g.email))
LEFT JOIN member m ON m.user_id = u.id AND m.workspace_id = w.id
WHERE w.id = @workspace_id
ORDER BY g.email;

-- name: IsLightsoftGuest :one
-- Whether user_id is listed as a guest of the workspace (uncached; for
-- enqueue-time checks).
SELECT EXISTS (
  SELECT 1
  FROM workspace w
  JOIN "user" u ON u.id = @user_id::uuid
  WHERE w.id = @workspace_id::uuid
    AND EXISTS (
      SELECT 1 FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(w.settings->'lightsoft_guests') = 'array'
        THEN w.settings->'lightsoft_guests' ELSE '[]'::jsonb END) AS g(email)
      WHERE lower(btrim(g.email)) = lower(u.email)
    )
)::bool AS guest;
