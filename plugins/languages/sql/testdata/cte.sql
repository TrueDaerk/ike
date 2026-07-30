with recent as (select * from orders where ts > now() - interval '7 days'), big as (select * from recent where total > 100) select big.id, big.total from big join users u on u.id = big.user_id;
