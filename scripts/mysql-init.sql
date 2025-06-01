-- MySQL initialization script for testing

-- Create test tables
CREATE TABLE IF NOT EXISTS users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    age INT,
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    metadata JSON
);

CREATE TABLE IF NOT EXISTS products (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    description TEXT,
    price DECIMAL(10,2) NOT NULL,
    category_id INT,
    in_stock BOOLEAN DEFAULT TRUE,
    tags JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS categories (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    parent_id INT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (parent_id) REFERENCES categories(id)
);

CREATE TABLE IF NOT EXISTS orders (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id INT,
    total_amount DECIMAL(10,2) NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    order_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS order_items (
    id INT AUTO_INCREMENT PRIMARY KEY,
    order_id INT,
    product_id INT,
    quantity INT NOT NULL,
    unit_price DECIMAL(10,2) NOT NULL,
    FOREIGN KEY (order_id) REFERENCES orders(id),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

-- Create indexes for better performance
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_active ON users(active);
CREATE INDEX idx_products_category ON products(category_id);
CREATE INDEX idx_orders_user ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_order_items_order ON order_items(order_id);
CREATE INDEX idx_order_items_product ON order_items(product_id);

-- Insert test data
INSERT IGNORE INTO categories (name, parent_id) VALUES 
    ('Electronics', NULL),
    ('Computers', 1),
    ('Smartphones', 1),
    ('Books', NULL),
    ('Fiction', 4),
    ('Non-Fiction', 4);

INSERT IGNORE INTO users (name, email, age, active, metadata) VALUES 
    ('John Doe', 'john.doe@example.com', 30, TRUE, '{"preferences": {"theme": "dark"}}'),
    ('Jane Smith', 'jane.smith@example.com', 25, TRUE, '{"preferences": {"theme": "light"}}'),
    ('Bob Johnson', 'bob.johnson@example.com', 35, FALSE, '{"preferences": {"theme": "auto"}}'),
    ('Alice Brown', 'alice.brown@example.com', 28, TRUE, '{"preferences": {"theme": "dark"}}'),
    ('Charlie Wilson', 'charlie.wilson@example.com', 42, TRUE, '{"preferences": {"theme": "light"}}');

INSERT IGNORE INTO products (name, description, price, category_id, tags) VALUES 
    ('MacBook Pro', 'High-performance laptop', 1999.99, 2, '["laptop", "apple", "professional"]'),
    ('iPhone 14', 'Latest smartphone from Apple', 999.99, 3, '["smartphone", "apple", "ios"]'),
    ('The Great Gatsby', 'Classic American novel', 12.99, 5, '["classic", "literature", "american"]'),
    ('Clean Code', 'Programming best practices', 45.99, 6, '["programming", "software", "development"]'),
    ('Samsung Galaxy S23', 'Android flagship phone', 899.99, 3, '["smartphone", "samsung", "android"]');

INSERT IGNORE INTO orders (user_id, total_amount, status) VALUES 
    (1, 1999.99, 'completed'),
    (2, 999.99, 'pending'),
    (3, 58.98, 'shipped'),
    (1, 899.99, 'completed'),
    (4, 12.99, 'pending');

INSERT IGNORE INTO order_items (order_id, product_id, quantity, unit_price) VALUES 
    (1, 1, 1, 1999.99),
    (2, 2, 1, 999.99),
    (3, 3, 1, 12.99),
    (3, 4, 1, 45.99),
    (4, 5, 1, 899.99),
    (5, 3, 1, 12.99);

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
