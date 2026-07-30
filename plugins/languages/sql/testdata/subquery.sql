select id, (select count(*) from orders o where o.user_id = u.id) as n from users u where u.id in (select user_id from orders where total > 50) and u.active = true;
