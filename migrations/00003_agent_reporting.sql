-- +goose Up
ALTER TABLE instance ADD COLUMN agent_version text;
