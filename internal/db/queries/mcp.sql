-- name: PruneMCPCalls :execrows
DELETE FROM mcp_calls WHERE at < ?;
