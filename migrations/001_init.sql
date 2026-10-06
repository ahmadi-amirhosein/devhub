CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role          text NOT NULL CHECK (role IN ('client', 'developer')),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       text NOT NULL,
    description text NOT NULL,
    category    text NOT NULL DEFAULT '',
    status      text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'open', 'in_progress', 'done')),
    budget_min  integer NOT NULL DEFAULT 0 CHECK (budget_min >= 0),
    budget_max  integer NOT NULL DEFAULT 0 CHECK (budget_max >= budget_min),
    spec        jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX projects_status_category_idx ON projects (status, category, created_at DESC);
CREATE INDEX projects_client_idx ON projects (client_id);
