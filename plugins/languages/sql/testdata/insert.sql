insert into users (name, email, plan_id) values ('ada', 'ada@example.com', 1), ('bob', 'bob@example.com', 2) returning id, name;
