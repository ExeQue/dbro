"""Build the demo shop database used for the README screenshots.

Deterministic: a fixed seed and fixed dates, so screenshots only change when the UI does.
Usage: python3 demo.py <path/to/shop.sqlite>
"""
import datetime
import json
import random
import sqlite3
import sys

random.seed(7)
db = sqlite3.connect(sys.argv[1])
db.executescript("""
PRAGMA foreign_keys = ON;
CREATE TABLE customers (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, email TEXT NOT NULL UNIQUE, country TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE products (id INTEGER PRIMARY KEY AUTOINCREMENT, sku TEXT NOT NULL UNIQUE, name TEXT NOT NULL, price NUMERIC NOT NULL, stock INTEGER NOT NULL DEFAULT 0, attributes TEXT);
CREATE TABLE orders (id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id INTEGER NOT NULL REFERENCES customers(id), status TEXT NOT NULL DEFAULT 'pending', total NUMERIC NOT NULL, note TEXT, created_at TEXT NOT NULL);
CREATE TABLE order_items (id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE, product_id INTEGER NOT NULL REFERENCES products(id), quantity INTEGER NOT NULL, unit_price NUMERIC NOT NULL);
CREATE INDEX orders_customer_id_index ON orders(customer_id);
CREATE INDEX orders_status_index ON orders(status);
CREATE VIEW revenue_by_country AS
  SELECT c.country, COUNT(o.id) AS orders, ROUND(SUM(o.total), 2) AS revenue
  FROM orders o JOIN customers c ON c.id = o.customer_id
  WHERE o.status <> 'canceled' GROUP BY c.country;
""")

first = ["Ada", "Linus", "Grace", "Ken", "Margaret", "Dennis", "Barbara", "Guido", "Radia", "Bjarne", "Frances", "Rob",
         "Hedy", "James", "Sophie", "Anders", "Yukihiro", "Karen", "Brian", "Evelyn", "Niklaus", "Joan", "Tim", "Ivan"]
last = ["Lovelace", "Torvalds", "Hopper", "Thompson", "Hamilton", "Ritchie", "Liskov", "van Rossum", "Perlman", "Stroustrup",
        "Allen", "Pike", "Lamarr", "Gosling", "Wilson", "Hejlsberg", "Matsumoto", "Sparck Jones", "Kernighan", "Berezin",
        "Wirth", "Clarke", "Berners-Lee", "Sutherland"]
countries = ["DK", "SE", "NO", "DE", "NL", "US", "GB", "FI"]
base = datetime.datetime(2026, 9, 1, 8, 0)
for i, (f, l) in enumerate(zip(first, last)):
    email = f"{f.lower()}.{l.lower().replace(' ', '')}@example.com"
    created = base + datetime.timedelta(days=i, hours=random.randint(0, 10))
    db.execute("INSERT INTO customers (name, email, country, created_at) VALUES (?, ?, ?, ?)",
               (f"{f} {l}", email, random.choice(countries), created.strftime("%Y-%m-%d %H:%M:%S")))

products = [
    ("KB-60", "Mechanical keyboard 60%", 129.0, {"switch": "brown", "layout": "ANSI", "rgb": True}),
    ("MS-ERG", "Ergonomic mouse", 59.5, {"dpi": 4000, "wireless": True}),
    ("MON-27", "27\" 4K monitor", 399.0, {"panel": "IPS", "hz": 144}),
    ("HUB-USB", "USB-C hub, 8 ports", 45.0, {"ports": 8, "pd_watt": 100}),
    ("DESK-SIT", "Sit-stand desk", 549.0, {"motors": 2, "width_cm": 160}),
    ("CHAIR-ERG", "Ergonomic chair", 389.0, {"lumbar": True}),
    ("MUG-DBRO", "dbro coffee mug", 14.0, {"ml": 350, "color": "red"}),
    ("HEAD-NC", "Noise-canceling headphones", 249.0, {"anc": True, "hours": 30}),
    ("CAM-4K", "4K webcam", 119.0, {"fps": 30}),
    ("LAMP-LED", "LED desk lamp", 39.0, {"kelvin": [2700, 6500]}),
]
for sku, name, price, attrs in products:
    db.execute("INSERT INTO products (sku, name, price, stock, attributes) VALUES (?, ?, ?, ?, ?)",
               (sku, name, price, random.randint(0, 120), json.dumps(attrs)))

statuses = ["paid"] * 6 + ["shipped"] * 5 + ["pending"] * 3 + ["canceled"]
notes = [None] * 8 + ["Gift wrap, please", "Leave at the back door", "Invoice to company", "Express shipping"]
for order in range(1, 181):
    customer = random.randint(1, len(first))
    at = base + datetime.timedelta(days=random.randint(0, 65), minutes=random.randint(0, 1440))
    items = [(random.randint(1, len(products)), random.randint(1, 3)) for _ in range(random.randint(1, 4))]
    total = round(sum(products[p - 1][2] * q for p, q in items), 2)
    db.execute("INSERT INTO orders (customer_id, status, total, note, created_at) VALUES (?, ?, ?, ?, ?)",
               (customer, random.choice(statuses), total, random.choice(notes), at.strftime("%Y-%m-%d %H:%M:%S")))
    for p, q in items:
        db.execute("INSERT INTO order_items (order_id, product_id, quantity, unit_price) VALUES (?, ?, ?, ?)",
                   (order, p, q, products[p - 1][2]))
db.commit()
