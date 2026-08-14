-- +goose Up
CREATE TYPE role_enum AS ENUM ('TANK', 'HEALER', 'MDPS', 'RDPS');
CREATE TYPE event_type AS ENUM ('RAID', 'MYTHIC_PLUS');
CREATE TYPE signup_status AS ENUM (
    'CONFIRMED', 'TENTATIVE', 'DECLINED', 'LATE', 'BENCH', 'ABSENT', 'NO_SHOW'
);
CREATE TYPE job_enum AS ENUM ('SIGNUP_DEADLINE', 'REMINDER_24H', 'REMINDER_1H', 'COMP_NAG');
CREATE TYPE job_status AS ENUM ('PENDING', 'SENT', 'FAILED', 'CANCELED');

-- +goose Down
DROP TYPE job_status;
DROP TYPE job_enum;
DROP TYPE signup_status;
DROP TYPE event_type;
DROP TYPE role_enum;
