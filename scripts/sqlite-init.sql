-- SQLite initialization script for testing

-- Create test tables
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT UNIQUE NOT NULL,
    age INTEGER,
    active BOOLEAN DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    metadata TEXT -- JSON stored as TEXT in SQLite
);

CREATE TABLE IF NOT EXISTS products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    price REAL NOT NULL,
    category_id INTEGER,
    in_stock BOOLEAN DEFAULT 1,
    tags TEXT, -- JSON array stored as TEXT
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    parent_id INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (parent_id) REFERENCES categories(id)
);

CREATE TABLE IF NOT EXISTS orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    total_amount REAL NOT NULL,
    status TEXT DEFAULT 'pending',
    order_date DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS order_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER,
    product_id INTEGER,
    quantity INTEGER NOT NULL,
    unit_price REAL NOT NULL,
    FOREIGN KEY (order_id) REFERENCES orders(id),
    FOREIGN KEY (product_id) REFERENCES products(id)
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
INSERT OR IGNORE INTO categories (name, parent_id) VALUES 
    ('Electronics', NULL),
    ('Computers', 1),
    ('Smartphones', 1),
    ('Books', NULL),
    ('Fiction', 4),
    ('Non-Fiction', 4);

INSERT OR IGNORE INTO users (name, email, age, active, metadata) VALUES 
    ('John Doe', 'john.doe@example.com', 30, 1, '{"preferences": {"theme": "dark"}}'),
    ('Jane Smith', 'jane.smith@example.com', 25, 1, '{"preferences": {"theme": "light"}}'),
    ('Bob Johnson', 'bob.johnson@example.com', 35, 0, '{"preferences": {"theme": "auto"}}'),
    ('Alice Brown', 'alice.brown@example.com', 28, 1, '{"preferences": {"theme": "dark"}}'),
    ('Charlie Wilson', 'charlie.wilson@example.com', 42, 1, '{"preferences": {"theme": "light"}}');

INSERT OR IGNORE INTO products (name, description, price, category_id, tags) VALUES 
    ('MacBook Pro', 'High-performance laptop', 1999.99, 2, '["laptop", "apple", "professional"]'),
    ('iPhone 14', 'Latest smartphone from Apple', 999.99, 3, '["smartphone", "apple", "ios"]'),
    ('The Great Gatsby', 'Classic American novel', 12.99, 5, '["classic", "literature", "american"]'),
    ('Clean Code', 'Programming best practices', 45.99, 6, '["programming", "software", "development"]'),
    ('Samsung Galaxy S23', 'Android flagship phone', 899.99, 3, '["smartphone", "samsung", "android"]');

INSERT OR IGNORE INTO orders (user_id, total_amount, status) VALUES 
    (1, 1999.99, 'completed'),
    (2, 999.99, 'pending'),
    (3, 58.98, 'shipped'),
    (1, 899.99, 'completed'),
    (4, 12.99, 'pending');

INSERT OR IGNORE INTO order_items (order_id, product_id, quantity, unit_price) VALUES 
    (1, 1, 1, 1999.99),
    (2, 2, 1, 999.99),
    (3, 3, 1, 12.99),
    (3, 4, 1, 45.99),
    (4, 5, 1, 899.99),
    (5, 3, 1, 12.99);

-- Create a view for order summaries
CREATE VIEW IF NOT EXISTS order_summaries AS
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
