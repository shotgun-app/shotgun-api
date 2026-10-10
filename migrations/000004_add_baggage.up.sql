-- Bag capacity is counted per size and checked independently: a ride's small bags never absorb large ones.
-- Default 0 keeps existing rides and bookings valid (no bag space offered, none requested).
ALTER TABLE rides
    ADD COLUMN small_bags_total integer NOT NULL DEFAULT 0 CHECK (small_bags_total >= 0),
    ADD COLUMN large_bags_total integer NOT NULL DEFAULT 0 CHECK (large_bags_total >= 0);

ALTER TABLE bookings
    ADD COLUMN small_bags integer NOT NULL DEFAULT 0 CHECK (small_bags >= 0),
    ADD COLUMN large_bags integer NOT NULL DEFAULT 0 CHECK (large_bags >= 0);
