SELECT 
  COALESCE((SELECT json_agg(json_build_object(
    'id', pu.id, 
    'name', pu.name, 
    'joinedAt', pu.created_at,
    'reviewRating', (
      SELECT rv.rating FROM reviews rv 
      WHERE rv.ride_id = r.id AND rv.reviewee_id = pu.id AND rv.reviewer_id = r.driver_id LIMIT 1
    )
  ) ORDER BY pb.created_at)
    FROM bookings pb JOIN users pu ON pu.id = pb.passenger_id
    WHERE pb.ride_id = r.id AND pb.status = 'confirmed'), '[]') AS passengers
FROM rides r LIMIT 1;
