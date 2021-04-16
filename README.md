# Service deployed on docker
# Depends on the database & table being available

# run app
run with docker-compose up -d

# confirm/create db tables 
# connect to container
docker exec -it objects bash
# connect to psql instance and create table
psql -h localhost -p 5432 -U postgres
create database objects;
\c objects
create table objects (id integer, online bool, lastseen character varying);
