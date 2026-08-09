-- Reverse 0033.
--
-- The seeded rows are removed in dependency order, and only for Nigeria: this
-- migration is the only thing that put them there. A postcode that has since
-- been referenced by a shipment address, an operating unit or a zone mapping
-- would fail the delete with a foreign key violation, which is the correct
-- outcome — silently orphaning routing data to make a rollback succeed would
-- be worse than a rollback that stops and says why.

DELETE FROM localities l
USING pincodes p, countries c
WHERE l.pincode_id = p.id AND p.country_id = c.id AND c.iso2 = 'NG';

DELETE FROM pincodes p
USING countries c
WHERE p.country_id = c.id AND c.iso2 = 'NG';

DELETE FROM cities ct
USING states s, countries c
WHERE ct.state_id = s.id AND s.country_id = c.id AND c.iso2 = 'NG';

DELETE FROM districts d
USING states s, countries c
WHERE d.state_id = s.id AND s.country_id = c.id AND c.iso2 = 'NG';

DROP INDEX IF EXISTS pincodes_district_idx;
DROP INDEX IF EXISTS pincodes_office_trgm_idx;
DROP INDEX IF EXISTS districts_name_trgm_idx;
DROP INDEX IF EXISTS cities_name_trgm_idx;
DROP INDEX IF EXISTS localities_name_trgm_idx;

-- pg_trgm is left installed: it is cheap, and dropping an extension another
-- migration may later depend on is not worth the tidiness.
