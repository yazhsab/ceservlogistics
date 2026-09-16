-- name: CreateShipmentCommercialSnapshot :exec
INSERT INTO shipment_commercial_snapshots(shipment_id, organization_id, insurance, customs, billing, transport_customer_id)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetShipmentCommercialSnapshot :one
SELECT insurance, customs, billing, created_at, transport_customer_id FROM shipment_commercial_snapshots
WHERE organization_id = $1 AND shipment_id = $2;
