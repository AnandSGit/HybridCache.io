// MongoDB initialization script for testing

// Switch to test database
db = db.getSiblingDB('testdb');

// Create collections and insert test data

// Users collection
db.users.insertMany([
    {
        _id: ObjectId(),
        name: "John Doe",
        email: "john.doe@example.com",
        age: 30,
        active: true,
        created_at: new Date(),
        updated_at: new Date(),
        metadata: {
            preferences: {
                theme: "dark"
            }
        }
    },
    {
        _id: ObjectId(),
        name: "Jane Smith",
        email: "jane.smith@example.com",
        age: 25,
        active: true,
        created_at: new Date(),
        updated_at: new Date(),
        metadata: {
            preferences: {
                theme: "light"
            }
        }
    },
    {
        _id: ObjectId(),
        name: "Bob Johnson",
        email: "bob.johnson@example.com",
        age: 35,
        active: false,
        created_at: new Date(),
        updated_at: new Date(),
        metadata: {
            preferences: {
                theme: "auto"
            }
        }
    },
    {
        _id: ObjectId(),
        name: "Alice Brown",
        email: "alice.brown@example.com",
        age: 28,
        active: true,
        created_at: new Date(),
        updated_at: new Date(),
        metadata: {
            preferences: {
                theme: "dark"
            }
        }
    },
    {
        _id: ObjectId(),
        name: "Charlie Wilson",
        email: "charlie.wilson@example.com",
        age: 42,
        active: true,
        created_at: new Date(),
        updated_at: new Date(),
        metadata: {
            preferences: {
                theme: "light"
            }
        }
    }
]);

// Categories collection
db.categories.insertMany([
    {
        _id: ObjectId(),
        name: "Electronics",
        parent_id: null,
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "Computers",
        parent_id: ObjectId(),
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "Smartphones",
        parent_id: ObjectId(),
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "Books",
        parent_id: null,
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "Fiction",
        parent_id: ObjectId(),
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "Non-Fiction",
        parent_id: ObjectId(),
        created_at: new Date()
    }
]);

// Products collection
db.products.insertMany([
    {
        _id: ObjectId(),
        name: "MacBook Pro",
        description: "High-performance laptop",
        price: 1999.99,
        category_id: ObjectId(),
        in_stock: true,
        tags: ["laptop", "apple", "professional"],
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "iPhone 14",
        description: "Latest smartphone from Apple",
        price: 999.99,
        category_id: ObjectId(),
        in_stock: true,
        tags: ["smartphone", "apple", "ios"],
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "The Great Gatsby",
        description: "Classic American novel",
        price: 12.99,
        category_id: ObjectId(),
        in_stock: true,
        tags: ["classic", "literature", "american"],
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "Clean Code",
        description: "Programming best practices",
        price: 45.99,
        category_id: ObjectId(),
        in_stock: true,
        tags: ["programming", "software", "development"],
        created_at: new Date()
    },
    {
        _id: ObjectId(),
        name: "Samsung Galaxy S23",
        description: "Android flagship phone",
        price: 899.99,
        category_id: ObjectId(),
        in_stock: true,
        tags: ["smartphone", "samsung", "android"],
        created_at: new Date()
    }
]);

// Orders collection
db.orders.insertMany([
    {
        _id: ObjectId(),
        user_id: ObjectId(),
        total_amount: 1999.99,
        status: "completed",
        order_date: new Date(),
        items: [
            {
                product_id: ObjectId(),
                quantity: 1,
                unit_price: 1999.99
            }
        ]
    },
    {
        _id: ObjectId(),
        user_id: ObjectId(),
        total_amount: 999.99,
        status: "pending",
        order_date: new Date(),
        items: [
            {
                product_id: ObjectId(),
                quantity: 1,
                unit_price: 999.99
            }
        ]
    },
    {
        _id: ObjectId(),
        user_id: ObjectId(),
        total_amount: 58.98,
        status: "shipped",
        order_date: new Date(),
        items: [
            {
                product_id: ObjectId(),
                quantity: 1,
                unit_price: 12.99
            },
            {
                product_id: ObjectId(),
                quantity: 1,
                unit_price: 45.99
            }
        ]
    }
]);

// Create indexes for better performance
db.users.createIndex({ "email": 1 }, { unique: true });
db.users.createIndex({ "active": 1 });
db.products.createIndex({ "category_id": 1 });
db.products.createIndex({ "tags": 1 });
db.orders.createIndex({ "user_id": 1 });
db.orders.createIndex({ "status": 1 });
db.orders.createIndex({ "order_date": 1 });

// Create text indexes for search
db.products.createIndex({ 
    "name": "text", 
    "description": "text", 
    "tags": "text" 
});

db.users.createIndex({ 
    "name": "text", 
    "email": "text" 
});

print("MongoDB test data initialized successfully!");
