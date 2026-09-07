CREATE TABLE workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE clients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    youtube_refresh_token TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE tracking_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID REFERENCES clients(id) ON DELETE CASCADE,
    slug VARCHAR(50) UNIQUE NOT NULL,
    target_url TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE clicks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    link_id UUID REFERENCES tracking_links(id) ON DELETE CASCADE,
    trakyo_id VARCHAR(50) UNIQUE NOT NULL,
    ip_hash VARCHAR(64),
    user_agent TEXT,
    geo_country VARCHAR(2),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE conversions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trakyo_id VARCHAR(50) REFERENCES clicks(trakyo_id) ON DELETE SET NULL,
    event_type VARCHAR(50) NOT NULL,
    revenue_amount DECIMAL(10, 2) DEFAULT 0.00,
    created_at TIMESTAMPTZ DEFAULT NOW()
);