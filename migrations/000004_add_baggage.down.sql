ALTER TABLE bookings
    DROP COLUMN large_bags,
    DROP COLUMN small_bags;

ALTER TABLE rides
    DROP COLUMN large_bags_total,
    DROP COLUMN small_bags_total;
