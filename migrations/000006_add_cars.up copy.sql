CREATE TABLE cars (
    id     uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    brand  varchar(60)  NOT NULL,
    model  varchar(100) NOT NULL,
    -- grams of CO2 per km; 0 for fully electric cars
    co2    integer      NOT NULL CHECK (co2 >= 0)
);

-- The same model cannot be listed twice
CREATE UNIQUE INDEX cars_brand_model_idx ON cars (lower(brand), lower(model));

-- SET NULL so removing a car from the catalogue never deletes a user
ALTER TABLE users
    ADD COLUMN car_id uuid REFERENCES cars (id) ON DELETE SET NULL;