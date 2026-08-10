-- +goose Up
DROP TABLE IF EXISTS graph_edges;
DROP TABLE IF EXISTS graph_nodes;
DROP TABLE IF EXISTS learning_goals;

-- +goose Down
-- Recreated by 009_knowledge_explorer.sql if needed
