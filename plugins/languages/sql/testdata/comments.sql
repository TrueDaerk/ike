-- daily revenue
select day, sum(total) -- gross
from orders
/* only paid
   orders count */
where paid = true
group by day; -- per day

-- cleanup
delete from sessions where expired = true;
