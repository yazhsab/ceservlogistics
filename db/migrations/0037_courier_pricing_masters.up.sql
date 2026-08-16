-- User-friendly pricing masters modelled after the legacy courier module.
-- The versioned lane engine remains authoritative; state rates are a safe
-- fallback when a more-specific zone lane or weight slab is not configured.
CREATE TABLE state_base_rates (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (is_public_id(public_id, 'sbr')),
    organization_id bigint NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    state_id bigint NOT NULL REFERENCES states(id) ON DELETE RESTRICT,
    regional_zone_id bigint REFERENCES zones(id) ON DELETE RESTRICT,
    courier_service_id bigint REFERENCES courier_services(id) ON DELETE CASCADE,
    base_weight_grams integer NOT NULL DEFAULT 500 CHECK (base_weight_grams > 0),
    base_cost_minor bigint NOT NULL CHECK (base_cost_minor >= 0),
    additional_step_grams integer NOT NULL DEFAULT 500 CHECK (additional_step_grams > 0),
    additional_cost_minor bigint NOT NULL DEFAULT 0 CHECK (additional_cost_minor >= 0),
    currency char(3) NOT NULL DEFAULT 'NGN',
    is_active boolean NOT NULL DEFAULT true,
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT state_base_rates_unique UNIQUE NULLS NOT DISTINCT
        (organization_id, state_id, courier_service_id)
);
CREATE INDEX state_base_rates_lookup_idx ON state_base_rates
    (organization_id, state_id, courier_service_id, is_active);
CREATE TRIGGER state_base_rates_touch BEFORE UPDATE ON state_base_rates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE package_types (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (is_public_id(public_id, 'pgt')),
    organization_id bigint NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    code text NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
    name text NOT NULL,
    length_mm integer NOT NULL DEFAULT 0 CHECK (length_mm >= 0),
    width_mm integer NOT NULL DEFAULT 0 CHECK (width_mm >= 0),
    height_mm integer NOT NULL DEFAULT 0 CHECK (height_mm >= 0),
    volumetric_divisor integer NOT NULL DEFAULT 5000 CHECK (volumetric_divisor > 0),
    max_weight_grams integer CHECK (max_weight_grams IS NULL OR max_weight_grams > 0),
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT package_types_code_unique UNIQUE (organization_id, code)
);
CREATE TRIGGER package_types_touch BEFORE UPDATE ON package_types
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Six Nigerian geopolitical pricing zones exposed by the legacy module.
INSERT INTO zones(public_id,organization_id,code,name,zone_type,description,sort_order,status)
SELECT gen_seed_public_id('zn'),o.id,v.code,v.name,'REGIONAL','Nigeria geopolitical pricing zone',v.ord,'ACTIVE'
FROM organizations o CROSS JOIN (VALUES
 ('NG-NC','North Central',10),('NG-NE','North East',20),('NG-NW','North West',30),
 ('NG-SE','South East',40),('NG-SS','South South (Niger Delta)',50),('NG-SW','South West',60)
) v(code,name,ord) ON CONFLICT (organization_id,code) DO NOTHING;

-- State prices repurposed from the staging courier module. Values are kobo.
INSERT INTO state_base_rates(public_id,organization_id,state_id,regional_zone_id,
    base_weight_grams,base_cost_minor,additional_step_grams,additional_cost_minor,currency)
SELECT gen_seed_public_id('sbr'),o.id,s.id,z.id,500,v.naira*100,500,0,o.currency
FROM organizations o
JOIN countries c ON c.iso2='NG'
CROSS JOIN (VALUES
 ('AB','NG-SE',10700),('AD','NG-NE',20300),('AK','NG-SS',10700),('AN','NG-SE',15300),
 ('BA','NG-NE',20300),('BY','NG-SS',15300),('BE','NG-NC',20300),('BO','NG-NE',20300),
 ('CR','NG-SS',3500),('DE','NG-SS',20300),('EB','NG-SE',15300),('ED','NG-SS',15300),
 ('EK','NG-SW',20300),('EN','NG-SE',15300),('GO','NG-NE',20300),('IM','NG-SE',15300),
 ('JI','NG-NW',20300),('KD','NG-NW',20300),('KN','NG-NW',20300),('KT','NG-NW',20300),
 ('KE','NG-NW',20300),('KO','NG-NC',20300),('KW','NG-NC',20300),('LA','NG-SW',15300),
 ('NA','NG-NC',20300),('NI','NG-NC',20300),('OG','NG-SW',20300),('ON','NG-SW',20200),
 ('OS','NG-SW',20300),('OY','NG-SW',20300),('PL','NG-NC',20300),('RI','NG-SS',15300),
 ('SO','NG-NW',20300),('TA','NG-NE',20300),('YO','NG-NE',20300),('ZA','NG-NW',20300),
 ('FC','NG-NC',20300)
) v(state_code,zone_code,naira)
JOIN states s ON s.country_id=c.id AND s.code=v.state_code
JOIN zones z ON z.organization_id=o.id AND z.code=v.zone_code
ON CONFLICT (organization_id,state_id,courier_service_id) DO NOTHING;

INSERT INTO package_types(public_id,organization_id,code,name,length_mm,width_mm,height_mm,volumetric_divisor,max_weight_grams)
SELECT gen_seed_public_id('pgt'),o.id,v.code,v.name,v.l,v.w,v.h,5000,v.max_weight
FROM organizations o CROSS JOIN (VALUES
 ('EXPRESS_ENVELOPE','Ceserv Express Envelope',0,0,0,1000),
 ('EXPRESS_PAK','Ceserv Express PAK',200,300,500,5000),
 ('EXPRESS_BOX','Ceserv Express Box',0,0,0,NULL),
 ('BOX_10KG','Ceserv 10 Kg Box',0,0,0,10000),
 ('BOX_25KG','Ceserv 25 Kg Box',0,0,0,25000),
 ('EXPRESS_TUBE','Ceserv Express Tube',0,0,0,NULL),
 ('PARCEL','Parcel / customer packaging',0,0,0,NULL)
) v(code,name,l,w,h,max_weight)
ON CONFLICT (organization_id,code) DO NOTHING;
