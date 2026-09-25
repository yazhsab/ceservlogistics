-- Country rows are shared reference data and may be referenced by shipments,
-- addresses or customs declarations after this migration. Removing them during
-- rollback would destroy those relationships, so rollback deliberately retains
-- the catalogue.
SELECT 1;
