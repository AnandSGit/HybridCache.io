-- PostgreSQL initialization script for testing

-- Create test tables
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    age INTEGER,
    active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    metadata JSONB
);

CREATE TABLE IF NOT EXISTS products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    description TEXT,
    price DECIMAL(10,2) NOT NULL,
    category_id INTEGER,
    in_stock BOOLEAN DEFAULT true,
    tags TEXT[],
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    parent_id INTEGER REFERENCES categories(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    total_amount DECIMAL(10,2) NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    order_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS order_items (
    id SERIAL PRIMARY KEY,
    order_id INTEGER REFERENCES orders(id),
    product_id INTEGER REFERENCES products(id),
    quantity INTEGER NOT NULL,
    unit_price DECIMAL(10,2) NOT NULL
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_active ON users(active);
CREATE INDEX IF NOT EXISTS idx_products_category ON products(category_id);
CREATE INDEX IF NOT EXISTS idx_orders_user ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_order_items_order ON order_items(order_id);
CREATE INDEX IF NOT EXISTS idx_order_items_product ON order_items(product_id);

-- Insert test data
INSERT INTO categories (name, parent_id) VALUES 
    ('Electronics', NULL),
    ('Computers', 1),
    ('Smartphones', 1),
    ('Books', NULL),
    ('Fiction', 4),
    ('Non-Fiction', 4)
ON CONFLICT DO NOTHING;

INSERT INTO users (name, email, age, active, metadata) VALUES 
    ('John Doe', 'john.doe@example.com', 30, true, '{"preferences": {"theme": "dark"}}'),
    ('Jane Smith', 'jane.smith@example.com', 25, true, '{"preferences": {"theme": "light"}}'),
    ('Bob Johnson', 'bob.johnson@example.com', 35, false, '{"preferences": {"theme": "auto"}}'),
    ('Alice Brown', 'alice.brown@example.com', 28, true, '{"preferences": {"theme": "dark"}}'),
    ('Charlie Wilson', 'charlie.wilson@example.com', 42, true, '{"preferences": {"theme": "light"}}')
ON CONFLICT (email) DO NOTHING;

INSERT INTO products (name, description, price, category_id, tags) VALUES 
    ('MacBook Pro', 'High-performance laptop', 1999.99, 2, ARRAY['laptop', 'apple', 'professional']),
    ('iPhone 14', 'Latest smartphone from Apple', 999.99, 3, ARRAY['smartphone', 'apple', 'ios']),
    ('The Great Gatsby', 'Classic American novel', 12.99, 5, ARRAY['classic', 'literature', 'american']),
    ('Clean Code', 'Programming best practices', 45.99, 6, ARRAY['programming', 'software', 'development']),
    ('Samsung Galaxy S23', 'Android flagship phone', 899.99, 3, ARRAY['smartphone', 'samsung', 'android'])
ON CONFLICT DO NOTHING;

INSERT INTO orders (user_id, total_amount, status) VALUES 
    (1, 1999.99, 'completed'),
    (2, 999.99, 'pending'),
    (3, 58.98, 'shipped'),
    (1, 899.99, 'completed'),
    (4, 12.99, 'pending')
ON CONFLICT DO NOTHING;

INSERT INTO order_items (order_id, product_id, quantity, unit_price) VALUES 
    (1, 1, 1, 1999.99),
    (2, 2, 1, 999.99),
    (3, 3, 1, 12.99),
    (3, 4, 1, 45.99),
    (4, 5, 1, 899.99),
    (5, 3, 1, 12.99)
ON CONFLICT DO NOTHING;

-- Create a function for updating timestamps
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger for users table
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Create a view for order summaries
CREATE OR REPLACE VIEW order_summaries AS
SELECT 
    o.id,
    u.name as customer_name,
    u.email as customer_email,
    o.total_amount,
    o.status,
    o.order_date,
    COUNT(oi.id) as item_count
FROM orders o
JOIN users u ON o.user_id = u.id
LEFT JOIN order_items oi ON o.id = oi.order_id
GROUP BY o.id, u.name, u.email, o.total_amount, o.status, o.order_date;

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO testuser;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO testuser;
GRANT ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public TO testuser;
