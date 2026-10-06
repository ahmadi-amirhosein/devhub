CREATE TABLE developer_profiles (
    user_id     uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    headline    text NOT NULL,
    bio         text NOT NULL DEFAULT '',
    skills      text[] NOT NULL DEFAULT '{}',
    hourly_rate integer NOT NULL DEFAULT 0 CHECK (hourly_rate >= 0),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE proposals (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    developer_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    price        integer NOT NULL CHECK (price > 0),
    message      text NOT NULL,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected', 'withdrawn')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, developer_id)
);

CREATE INDEX proposals_developer_idx ON proposals (developer_id, created_at DESC);
