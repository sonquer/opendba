CREATE TABLE users (id integer primary key, email text not null, created_at text);

CREATE TABLE orders (
  id integer primary key,
  user_id integer references users(id),
  total integer,
  currency text,
  status text,
  placed_at text,
  note text
);

CREATE INDEX orders_user_id ON orders(user_id);
CREATE INDEX orders_status ON orders(status);

INSERT INTO users (email, created_at) VALUES
  ('ada@example.com', '2026-01-04'),
  ('grace@example.com', '2026-02-11'),
  ('alan@example.com', '2026-03-19');

INSERT INTO orders (user_id, total, currency, status, placed_at, note) VALUES
  (1, 4200, 'PLN', 'paid', '2026-08-01', 'first'),
  (2, 199, 'EUR', 'pending', '2026-08-02', 'second'),
  (3, 15000, 'PLN', 'paid', '2026-08-03', 'third');
