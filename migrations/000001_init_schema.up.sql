CREATE TYPE booking_status AS ENUM ('confirmed', 'cancelled');

CREATE TABLE users (
    id            uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    name          varchar(100) NOT NULL,
    email         varchar(320) NOT NULL UNIQUE,
    password_hash varchar(255) NOT NULL,
    phone         varchar(20),
    created_at    timestamptz  NOT NULL DEFAULT now(),
    updated_at    timestamptz
);

CREATE TABLE sessions (
    id         uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token      varchar(255) NOT NULL UNIQUE,
    expires_at timestamptz  NOT NULL,
    created_at timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE rides (
    id                  uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id           uuid          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    origin_city         varchar(100)  NOT NULL,
    origin_country      varchar(100)  NOT NULL,
    destination_city    varchar(100)  NOT NULL,
    destination_country varchar(100)  NOT NULL,
    departure_at        timestamptz   NOT NULL,
    seats_total         integer       NOT NULL CHECK (seats_total > 0),
    price_per_seat      numeric(8, 2) NOT NULL CHECK (price_per_seat >= 0),
    currency            varchar(3)    NOT NULL DEFAULT 'EUR',
    notes               varchar(500)  NOT NULL DEFAULT '',
    created_at          timestamptz   NOT NULL DEFAULT now(),
    updated_at          timestamptz
);

CREATE INDEX rides_driver_id_idx ON rides (driver_id);
CREATE INDEX rides_search_idx ON rides (origin_city, destination_city, departure_at);

CREATE TABLE bookings (
    id           uuid           PRIMARY KEY DEFAULT gen_random_uuid(),
    ride_id      uuid           NOT NULL REFERENCES rides (id) ON DELETE CASCADE,
    passenger_id uuid           NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    seats        integer        NOT NULL DEFAULT 1 CHECK (seats > 0),
    status       booking_status NOT NULL DEFAULT 'confirmed',
    created_at   timestamptz    NOT NULL DEFAULT now(),
    updated_at   timestamptz,
    UNIQUE (ride_id, passenger_id)
);

CREATE INDEX bookings_passenger_id_idx ON bookings (passenger_id);
