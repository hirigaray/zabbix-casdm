create table if not exists events (
	id        serial   primary key,
	zabbix_id integer  not null unique,
	ca_id     integer           unique,
	ca_status char(15)          unique
);

create table if not exists triggers (
	id                  serial    primary key,
	host_id             integer   not null,
	host_name           char(30)  not null,
	trigger_id          integer   not null unique,
	trigger_description char(100) not null,
	send_to_ca          bool      not null
);
