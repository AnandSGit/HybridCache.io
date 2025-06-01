-- PostgreSQL initialization script for Database Agnostic Storage Library (Python)

-- Create additional test databases if needed
-- CREATE DATABASE testdb2;

-- Create extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_stat_statements";

-- Grant permissions
GRANT ALL PRIVILEGES ON DATABASE testdb TO testuser;

-- Connect to testdb
\c testdb;

-- Create test schema
CREATE SCHEMA IF NOT EXISTS test_schema;
GRANT ALL ON SCHEMA test_schema TO testuser;

-- Create sample tables for testing
CREATE TABLE IF NOT EXISTS sample_users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    age INTEGER DEFAULT 0,
    active BOOLEAN DEFAULT true,
    metadata JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_sample_users_email ON sample_users(email);
CREATE INDEX IF NOT EXISTS idx_sample_users_active ON sample_users(active);
CREATE INDEX IF NOT EXISTS idx_sample_users_metadata ON sample_users USING GIN(metadata);

-- Insert sample data
INSERT INTO sample_users (name, email, age, metadata) VALUES
    ('Alice Johnson', 'alice@example.com', 28, '{"department": "engineering", "role": "developer"}'),
    ('Bob Smith', 'bob@example.com', 35, '{"department": "sales", "role": "manager"}'),
    ('Carol Davis', 'carol@example.com', 42, '{"department": "marketing", "role": "director"}')
ON CONFLICT (email) DO NOTHING;

-- Grant permissions on tables
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO testuser;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO testuser;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA test_schema TO testuser;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA test_schema TO testuser;

-- Create function for updated_at trigger
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger for updated_at
CREATE TRIGGER update_sample_users_updated_at 
    BEFORE UPDATE ON sample_users 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();
