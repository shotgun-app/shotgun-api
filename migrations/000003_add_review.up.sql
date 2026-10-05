CREATE TABLE reviews (
    id          uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    ride_id     uuid         NOT NULL REFERENCES rides (id) ON DELETE CASCADE,
    reviewer_id uuid         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reviewee_id uuid         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    rating      integer      NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment     varchar(500) NOT NULL DEFAULT '',
    created_at  timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT reviews_reviewer_reviewee_diff CHECK (reviewer_id <> reviewee_id)
);

CREATE INDEX reviews_reviewee_id_idx ON reviews (reviewee_id);

-- One review per ride, per reviewer -> reviewee pair (prevents double-rating the same trip)
CREATE UNIQUE INDEX reviews_once_per_ride_idx ON reviews (ride_id, reviewer_id, reviewee_id);