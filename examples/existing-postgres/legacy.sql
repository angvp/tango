-- The billing database as an older tool created it, before tanGO. The models
-- in ledger/ describe exactly this: 4-byte SERIAL keys, timestamp without
-- time zone, NOT NULL, column defaults and explicit index names.

CREATE TABLE customers (
    id         serial PRIMARY KEY,
    name       varchar(80)  NOT NULL,
    email      varchar(255) NOT NULL,
    tier       varchar(20)  NOT NULL DEFAULT 'standard',
    since      date         NOT NULL,
    created_at timestamp without time zone NOT NULL
);
CREATE UNIQUE INDEX idx_customers_email ON customers (email);

CREATE TABLE invoices (
    id           serial PRIMARY KEY,
    customer_id  integer NOT NULL REFERENCES customers (id),
    amount_cents integer NOT NULL DEFAULT 0,
    issued_on    date    NOT NULL,
    paid_at      timestamp without time zone
);
CREATE INDEX idx_invoices_customer_id ON invoices (customer_id);
